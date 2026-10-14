package handlers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"tableTop/main/internal/app/model"
	"tableTop/main/internal/app/persistence"
	"tableTop/main/internal/plugins"
)

func largeCatalogHandler(t *testing.T) *EndpointHandler {
	t.Helper()
	library, err := persistence.NewUnitLibrary(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, pack := range plugins.Discover([]string{testAssetRoot(t) + "/plugins"}, plugins.Bounds{Metadata: 1 << 20, Content: 16 << 20}) {
		if pack.ID == "dnd-5e" {
			library.SetDefaults(pack)
		}
	}
	state := model.NewState()
	return newTestEndpointHandler(t, state, NewWebSocketHandler(state), library)
}

func TestLargeCatalogMenuBoundsAndSearch(t *testing.T) {
	h := largeCatalogHandler(t)
	data := h.menuData(true)
	if !data.ServerSearch || data.NPCs.DefaultTotal() != 3617 || len(data.NPCs.Defaults) != unitMenuGroupLimit {
		t.Fatalf("invalid bounded catalog: total=%d shown=%d", data.NPCs.Total(), len(data.NPCs.Defaults))
	}
	start := time.Now()
	response := httptest.NewRecorder()
	h.renderSpawnMenu(response, data)
	boundedTime := time.Since(start)
	boundedSize := response.Body.Len()
	all := data
	all.NPCs.Defaults = h.library.Units()
	all.Shown = len(all.NPCs.Defaults)
	start = time.Now()
	full := httptest.NewRecorder()
	h.renderSpawnMenu(full, all)
	t.Logf("menu render: full %s / %d bytes; bounded %s / %d bytes", time.Since(start), full.Body.Len(), boundedTime, boundedSize)
	if boundedSize >= full.Body.Len()/10 {
		t.Fatal("menu payload was not substantially reduced")
	}
	for query, expected := range map[string]string{"q=dragon+ancient": "Ancient", "q=zzzz-no-such-monster": "No units match your search."} {
		result := httptest.NewRecorder()
		h.spawnMenu(result, httptest.NewRequest(http.MethodGet, "/spawn-menu?open=true&"+query, nil))
		if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), expected) {
			t.Fatalf("search %s failed: %d", query, result.Code)
		}
	}
	page := h.searchMenuPageData(true, "", 1)
	if page.NPCs.Defaults[0].ID == data.NPCs.Defaults[0].ID || page.NPCs.DefaultTotal() != data.NPCs.DefaultTotal() {
		t.Fatal("paging lost counts or repeated the first results")
	}
	matches := h.searchMenuData(true, "dragon ancient")
	if matches.MatchCount() == 0 || !matches.NPCs.Expanded {
		t.Fatal("search did not find and expand later catalog entries")
	}
	for _, unit := range matches.NPCs.Defaults {
		if !strings.Contains(strings.ToLower(unit.Name), "dragon") || !strings.Contains(strings.ToLower(unit.Name), "ancient") {
			t.Fatalf("incorrect match: %s", unit.Name)
		}
	}
}

func TestBrowserLargeCatalogSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test disabled by -short")
	}
	firefox, err := exec.LookPath("firefox")
	if err != nil {
		t.Skip("Firefox is required")
	}
	h := largeCatalogHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(testAssetRoot(t)+"/static"))))
	results := make(chan string, 1)
	mux.HandleFunc("POST /browser/result", func(w http.ResponseWriter, r *http.Request) { results <- r.URL.Query().Get("error") })
	mux.HandleFunc("GET /browser", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<!doctype html><script src="/static/vendor/htmx-2.0.11.min.js"></script><body>`)
		h.renderSpawnMenu(w, h.menuData(true))
		io.WriteString(w, `<script src="/static/js/library/librarySearch.js"></script><script>
(async()=>{
 const wait=async(test)=>{for(let i=0;i<200;i++){if(test())return;await new Promise(r=>setTimeout(r,25));}throw new Error('Timed out: '+document.body.innerText);};
 try {
 await new Promise(r=>setTimeout(r,250));
 const initialCount=document.querySelectorAll('[data-library-name]').length;
 if(initialCount<60||initialCount>240)throw new Error('Initial results are not bounded');
 const search=value=>{const input=document.getElementById('unit-library-search');input.value=value;input.dispatchEvent(new Event('input',{bubbles:true}));};
 const first=document.querySelector('[data-library-defaults] [data-library-name]').dataset.libraryName;
 document.querySelector('.library-pagination button:last-child').click();
 await wait(()=>document.querySelector('[data-library-defaults] [data-library-name]')?.dataset.libraryName!==first);
 await new Promise(r=>setTimeout(r,50));
 search('dragon ancient');
 await wait(()=>document.querySelector('[data-library-defaults][open]')&&document.querySelector('[data-library-name]')?.dataset.libraryName.includes('Ancient'));
 const names=[...document.querySelectorAll('[data-library-name]')].map(e=>e.dataset.libraryName.toLowerCase());
 if(names.some(n=>!n.includes('dragon')||!n.includes('ancient')))throw new Error('Incorrect results');
 if(document.getElementById('unit-library-search').value!=='dragon ancient')throw new Error('Query lost during swap');
 search('zzzz-no-such-monster');
 await wait(()=>document.querySelector('[data-library-search-empty]')?.hidden===false);
 search('');
 await wait(()=>document.querySelectorAll('[data-library-name]').length===initialCount);
 fetch('/browser/result',{method:'POST'});
 }catch(error){fetch('/browser/result?error='+encodeURIComponent(error.message),{method:'POST'});}
})();</script>`)
	})
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/images/") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, firefox, "--headless", "--no-remote", "--profile", t.TempDir(), server.URL+"/browser")
	command.WaitDelay = 2 * time.Second
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { command.Process.Kill(); command.Wait() }()
	select {
	case message := <-results:
		if message != "" {
			t.Fatal(message)
		}
	case <-ctx.Done():
		t.Fatalf("browser search timeout: %s", output.String())
	}
}
