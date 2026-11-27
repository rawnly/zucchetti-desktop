package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

const portalLoginURL = "https://saas.hrzucchetti.it/hrpprogest/jsp/login.jsp"

// ErrPortalLogin means the portal did not let the user in: a wrong password,
// or a code the portal asks for.
var ErrPortalLogin = errors.New("could not sign in to Zucchetti: check the username and password, or the portal may be asking for a code")

// Portal reads payslips straight from the Zucchetti HR portal.
type Portal interface {
	// List returns every payslip the portal has, newest first.
	List(ctx context.Context) ([]Payslip, error)
	// Fetch saves payslips into dir as YYYY-MM.pdf, in one portal session.
	// each hears the outcome of every payslip as soon as it is known.
	Fetch(ctx context.Context, items []Payslip, dir string, each func(Payslip, error)) error
}

var (
	rowTitleRe = regexp.MustCompile(`^Libro unico di (\d{2})-(\d{4})`)
	// The date follows the folder's year with no space: "...\2026" and then
	// "4 Lug 2026 alle 19:02" read "...\20264 Lug 2026 alle 19:02". The year
	// is the four digits after the backslash, the day what is left.
	rowDateRe  = regexp.MustCompile(`\\\d{4}(\d{1,2}) (Gen|Feb|Mar|Apr|Mag|Giu|Lug|Ago|Set|Ott|Nov|Dic) (\d{4}) alle (\d{2}):(\d{2})\s*$`)
	rowDateAlt = regexp.MustCompile(`(?:^|[^\d])(\d{1,2}) (Gen|Feb|Mar|Apr|Mag|Giu|Lug|Ago|Set|Ott|Nov|Dic) (\d{4}) alle (\d{2}):(\d{2})\s*$`)
	itMonths   = map[string]time.Month{"Gen": 1, "Feb": 2, "Mar": 3, "Apr": 4, "Mag": 5, "Giu": 6, "Lug": 7, "Ago": 8, "Set": 9, "Ott": 10, "Nov": 11, "Dic": 12}
)

// parsePortalRows turns the texts of the portal's document rows into payslips,
// newest first. Only "Libro unico di MM-YYYY" rows count, and the first row
// of a month wins. A row's text also holds the employee's tax code, which is
// never read.
func parsePortalRows(rows []string) []Payslip {
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		rome = time.UTC
	}
	seen := map[string]bool{}
	var out []Payslip
	for _, r := range rows {
		m := rowTitleRe.FindStringSubmatch(strings.TrimSpace(r))
		if m == nil {
			continue
		}
		month, _ := strconv.Atoi(m[1])
		year, _ := strconv.Atoi(m[2])
		key := fmt.Sprintf("busta-%04d-%02d.pdf", year, month)
		if month < 1 || month > 12 || seen[key] || !IsValidBustaKey(key) {
			continue
		}
		seen[key] = true
		p := Payslip{Key: key, Year: year, Month: month}
		d := rowDateRe.FindStringSubmatch(r)
		if d == nil {
			d = rowDateAlt.FindStringSubmatch(r)
		}
		if d != nil {
			day, _ := strconv.Atoi(d[1])
			y, _ := strconv.Atoi(d[3])
			hh, _ := strconv.Atoi(d[4])
			mm, _ := strconv.Atoi(d[5])
			p.Uploaded = time.Date(y, itMonths[d[2]], day, hh, mm, 0, 0, rome)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key > out[j].Key })
	return out
}

// webPortal drives the portal in a web window that the user does not see.
type webPortal struct {
	user, pass string

	mu   sync.Mutex // one portal session at a time
	sess *portalSession
	idle *time.Timer
}

func newWebPortal(user, pass string) Portal { return &webPortal{user: user, pass: pass} }

// idleClose is how long a signed-in session is kept for the next request.
const idleClose = 90 * time.Second

func (w *webPortal) List(ctx context.Context) ([]Payslip, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	s, err := w.session(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.rows(ctx)
	if err != nil {
		w.drop()
		return nil, err
	}
	w.release()
	return parsePortalRows(rows), nil
}

func (w *webPortal) Fetch(ctx context.Context, items []Payslip, dir string, each func(Payslip, error)) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	s, err := w.session(ctx)
	if err != nil {
		return err
	}
	for _, p := range items {
		if err := ctx.Err(); err != nil {
			each(p, err)
			continue
		}
		each(p, s.save(ctx, p, dir))
	}
	w.release()
	return nil
}

// session returns the open session, or signs in to a new one.
func (w *webPortal) session(ctx context.Context) (*portalSession, error) {
	if w.idle != nil {
		w.idle.Stop()
	}
	if w.sess != nil {
		return w.sess, nil
	}
	s, err := openPortalSession(ctx, w.user, w.pass)
	if err != nil {
		return nil, err
	}
	w.sess = s
	return s, nil
}

// release keeps the session for a while, then closes its window.
func (w *webPortal) release() {
	w.idle = time.AfterFunc(idleClose, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.drop()
	})
}

func (w *webPortal) drop() {
	if w.idle != nil {
		w.idle.Stop()
	}
	if w.sess != nil {
		w.sess.close()
		w.sess = nil
	}
}

type portalSession struct {
	win  *mygo.Window
	page *mygo.Page
	seen map[string]bool // viewer URLs already read in this session
}

// docs lists the page's document and the documents of its same-origin frames.
const portalHelpers = `const docs=(r=document)=>{const o=[r];for(const f of r.querySelectorAll('iframe')){try{if(f.contentDocument)o.push(...docs(f.contentDocument))}catch(e){}}return o};`

// newGhostWindow opens the portal's login page in a window nobody sees: no
// frame, no taskbar entry, no focus, no mouse, fully transparent. It must
// count as shown, because WebKit does not run animations in a hidden window,
// and the portal opens its PDF viewer after one.
func newGhostWindow(ctx context.Context) (*mygo.Window, error) {
	ch := make(chan *mygo.Window, 1)
	mygo.RunOnMain(func() {
		w := mygo.NewWindow(mygo.WindowOptions{
			URL: portalLoginURL, Width: 1280, Height: 900,
			Hidden: true, Frameless: true, Transparent: true, Opacity: 0.01,
			IgnoreMouseEvents: true, SkipTaskbar: true,
		})
		w.ShowInactive()
		w.SetOpacity(0)
		ch <- w
	})
	select {
	case w := <-ch:
		return w, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Second):
		return nil, errors.New("could not open the portal window")
	}
}

func openPortalSession(ctx context.Context, user, pass string) (*portalSession, error) {
	if user == "" || pass == "" {
		return nil, errors.New("the Zucchetti username and password are not set")
	}
	win, err := newGhostWindow(ctx)
	if err != nil {
		return nil, err
	}
	s := &portalSession{win: win, page: win.Page(), seen: map[string]bool{}}
	if err := s.signIn(ctx, user, pass); err != nil {
		s.close()
		return nil, err
	}
	if err := s.openDocuments(ctx); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

func (s *portalSession) close() {
	w := s.win
	mygo.RunOnMain(func() { w.Destroy() })
}

func (s *portalSession) eval(ctx context.Context, code string) (any, error) {
	return s.page.EvalContext(ctx, portalHelpers+code)
}

// until runs code every half second until it returns true.
func (s *portalSession) until(ctx context.Context, what, code string, limit time.Duration) error {
	end := time.Now().Add(limit)
	for {
		if v, err := s.eval(ctx, code); err == nil && v == true {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Now().After(end) {
			return fmt.Errorf("the portal did not show %s in time", what)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (s *portalSession) signIn(ctx context.Context, user, pass string) error {
	if err := s.until(ctx, "its login form", `return !!document.querySelector('input[id$="_m_cUserName"]')`, 60*time.Second); err != nil {
		return err
	}
	u, _ := json.Marshal(user)
	p, _ := json.Marshal(pass)
	v, err := s.eval(ctx, fmt.Sprintf(`const U=%s,P=%s;
const set=(el,v)=>{el.focus();Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(el,v);for(const t of ['input','change','keyup'])el.dispatchEvent(new Event(t,{bubbles:true}))};
set(document.querySelector('input[id$="_m_cUserName"]'),U);set(document.querySelector('input[type="password"]'),P);
const b=document.querySelector('[id$="_Accedi"]');if(!b)return false;b.click();return true`, u, p))
	if err != nil || v != true {
		return errors.New("the portal's login form was not as expected")
	}
	if err := s.until(ctx, "the home page", `return !location.href.includes('login.jsp')`, 45*time.Second); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrPortalLogin
	}
	return nil
}

// openDocuments walks the portal's menus to the "I miei documenti" grid.
func (s *portalSession) openDocuments(ctx context.Context) error {
	time.Sleep(2 * time.Second)
	_, _ = s.eval(ctx, `for(const d of docs()){const m=d.querySelector('[id$="_imgMenu"]');if(m){m.click();return true}}return false`)
	time.Sleep(900 * time.Millisecond)
	_, _ = s.eval(ctx, `for(const d of docs()){const a=[...d.querySelectorAll('[id*="MenuView_AppArea"] a,[id*="MenuView_AppArea"] [id*="_descr"]')].find(x=>(x.textContent||'').trim()==='Workspace');if(a){a.click();return true}}return false`)
	if err := s.until(ctx, "the workspace", `return docs().some(d=>[...d.querySelectorAll('iframe')].some(f=>/^MyWorkspace_/.test(f.id)))`, 40*time.Second); err != nil {
		return err
	}
	time.Sleep(3 * time.Second)
	if err := s.until(ctx, "MySpace", `for(const d of docs()){const a=[...d.querySelectorAll('a,span,div[onclick],li')].find(x=>(x.textContent||'').trim()==='MySpace');if(a){a.click();return true}}return false`, 30*time.Second); err != nil {
		return err
	}
	time.Sleep(2500 * time.Millisecond)
	return s.until(ctx, "your documents", `for(const d of docs()){const a=[...d.querySelectorAll('a')].find(x=>(x.textContent||'').includes('I miei documenti')&&/execLink/.test(x.getAttribute('onclick')||''));if(a){a.click();return true}}return false`, 30*time.Second)
}

// rows reads the texts of the grid's document rows, once the list has
// stopped growing.
func (s *portalSession) rows(ctx context.Context) ([]string, error) {
	const js = `const out=[];for(const d of docs()){for(const a of d.querySelectorAll('a[onclick]')){if(/openDocument/.test(a.getAttribute('onclick')||''))out.push((a.textContent||'').trim().replace(/\s+/g,' '))}}return out`
	var last []string
	stable := 0
	for i := 0; i < 60; i++ {
		if v, err := s.eval(ctx, js); err == nil {
			var cur []string
			if list, ok := v.([]any); ok {
				for _, x := range list {
					if t, ok := x.(string); ok {
						cur = append(cur, t)
					}
				}
			}
			if len(cur) > 0 && len(cur) == len(last) {
				if stable++; stable >= 3 {
					return cur, nil
				}
			} else {
				stable = 0
			}
			last = cur
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		time.Sleep(700 * time.Millisecond)
	}
	if len(last) > 0 {
		return last, nil
	}
	return nil, errors.New("the portal shows no documents")
}

// save downloads one payslip's PDF into dir, checking that it is the month
// asked for: the portal sometimes serves another month's document, so a
// wrong one is fetched once more.
func (s *portalSession) save(ctx context.Context, p Payslip, dir string) error {
	name, ok := LocalFileName(p.Key)
	if !ok {
		return fmt.Errorf("not a payslip: %q", p.Key)
	}
	var data []byte
	for attempt := 0; attempt < 2; attempt++ {
		b, err := s.fetchPDF(ctx, p.Year, p.Month)
		if err != nil {
			return err
		}
		data = b
		if periodMatches(b, dir, p) {
			break
		}
	}
	_, err := writeFileAtomic(dir, name, data)
	return err
}

// periodMatches reports whether the PDF's printed period is the payslip's
// month. A PDF that cannot be read is given the benefit of the doubt.
func periodMatches(data []byte, dir string, p Payslip) bool {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return true
	}
	tmp, err := os.CreateTemp(dir, ".check-*.tmp")
	if err != nil {
		return true
	}
	defer os.Remove(tmp.Name())
	tmp.Write(data)
	tmp.Close()
	d, err := ExtractPayslip(filepath.Clean(tmp.Name()))
	if err != nil {
		return true
	}
	y, m, ok := d.PeriodYM()
	return !ok || (y == p.Year && m == p.Month)
}

// fetchPDF opens a month's row, waits for the viewer and reads its PDF. The
// portal opens the row that is selected, not the one whose link was clicked,
// and a plain click() does not select it: so the click is sent as a mouse
// would, with the button going down and up on the row first. Without that,
// every payslip but the first opens the first one's document.
func (s *portalSession) fetchPDF(ctx context.Context, year, month int) ([]byte, error) {
	grid := fmt.Sprintf("Libro unico di %02d-%04d", month, year)
	const viewers = `const found=[];for(const d of docs()){for(const m of d.querySelectorAll('iframe')){if(!(m.src||'').includes('wfolderem_document'))continue;const doc=m.contentDocument;if(!doc)continue;const title=((doc.body&&doc.body.innerText)||'').match(/Libro unico di \d{2}-\d{4}/);for(const v of doc.querySelectorAll('iframe')){const src=v.getAttribute('src')||'';if(!/viewer\.html/.test(src)||!src.includes('file='))continue;found.push({url:new URL(decodeURIComponent(src.split('file=')[1]),location.origin+'/hrpprogest/hrf_pdfviewer/web/').href,title:title?title[0]:''})}}}return found`
	g, _ := json.Marshal(grid)
	v, err := s.eval(ctx, fmt.Sprintf(`const g=%s;for(const d of docs()){const a=[...d.querySelectorAll('a[onclick]')].find(x=>(x.textContent||'').includes(g)&&/openDocument/.test(x.getAttribute('onclick')||''));if(a){const w=d.defaultView;const fire=t=>a.dispatchEvent(new w.MouseEvent(t,{bubbles:true,cancelable:true,view:w,button:0}));fire('mouseover');fire('mousedown');fire('mouseup');a.click();return true}}return false`, g))
	if err != nil {
		return nil, err
	}
	if v != true {
		return nil, fmt.Errorf("%s is not on the portal", grid)
	}
	var url string
	for i := 0; i < 80 && url == ""; i++ {
		if v, err := s.eval(ctx, viewers); err == nil {
			if list, ok := v.([]any); ok {
				for _, x := range list {
					m, _ := x.(map[string]any)
					u, _ := m["url"].(string)
					t, _ := m["title"].(string)
					if u != "" && t == grid && !s.seen[u] {
						url = u
					}
				}
			}
		}
		if url == "" {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	if url == "" {
		return nil, fmt.Errorf("the portal did not open %s", grid)
	}
	s.seen[url] = true
	u, _ := json.Marshal(url)
	v, err = s.eval(ctx, fmt.Sprintf(`const r=await fetch(%s,{credentials:'include'});const b=await r.blob();return await new Promise(res=>{const f=new FileReader();f.onload=()=>res(f.result.split(',')[1]);f.readAsDataURL(b)})`, u))
	if err != nil {
		return nil, err
	}
	b64, _ := v.(string)
	if !strings.HasPrefix(b64, "JVBERi") {
		return nil, errors.New("the portal did not return a PDF")
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	return raw, nil
}
