package tables

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"tableTop/main/internal/app"
	"tableTop/main/internal/auth"
	"tableTop/main/internal/plugins"
)

type cleanupFailureStore struct {
	err    error
	scopes []string
}

func (*cleanupFailureStore) Maps(string) app.DocumentStore[app.MapDefinition]   { return nil }
func (*cleanupFailureStore) Units(string) app.DocumentStore[app.UnitDefinition] { return nil }
func (s *cleanupFailureStore) DeleteScope(scope string) error {
	s.scopes = append(s.scopes, scope)
	return s.err
}

type discordTransport struct{}

func (discordTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var body string
	if strings.HasSuffix(request.URL.Path, "/token") {
		data, _ := io.ReadAll(request.Body)
		form, _ := url.ParseQuery(string(data))
		body = fmt.Sprintf(`{"access_token":%q,"token_type":"Bearer"}`, form.Get("code"))
	} else {
		id := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		body = fmt.Sprintf(`{"id":%q,"username":%q}`, id, id)
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": {"application/json"}}}, nil
}

type testSession struct{ path, id string }

func (f tableTestFixture) createSession(t *testing.T, client *http.Client, name string) testSession {
	t.Helper()
	response, _ := f.request(client, "POST", "/sessions", url.Values{"name": {name}, "plugin": {"builtin"}}.Encode(), http.StatusSeeOther)
	path := response.Header.Get("Location")
	return testSession{path, strings.Split(path, "/")[2]}
}

func (f tableTestFixture) joinSession(client *http.Client, session testSession) {
	f.request(client, "POST", "/sessions/join", "code="+session.id, http.StatusSeeOther)
}

func TestSessionCreatorGetsGMRoleAndRequestPrefix(t *testing.T) {
	f := newTableTestFixture(t)
	owner := f.login("owner")
	session := f.createSession(t, owner, "First")
	_, html := f.request(owner, "GET", session.path, "", http.StatusOK)
	for _, expected := range []string{`data-gm="true"`, `hx-get="` + session.path + `loadExistingUnits"`, `data-session-prefix="` + strings.TrimSuffix(session.path, "/") + `"`} {
		if !strings.Contains(html, expected) {
			t.Fatalf("board is missing %s", expected)
		}
	}
}

func TestNonmemberIsOfferedSessionJoinCode(t *testing.T) {
	f := newTableTestFixture(t)
	session := f.createSession(t, f.login("owner"), "First")
	response, _ := f.request(f.login("player"), "GET", session.path, "", http.StatusSeeOther)
	if !strings.Contains(response.Header.Get("Location"), "invite="+session.id) {
		t.Fatal("nonmember was not offered the join code")
	}
}

func TestJoinedPlayerCannotElevateGMRole(t *testing.T) {
	f := newTableTestFixture(t)
	owner, player := f.login("owner"), f.login("player")
	session := f.createSession(t, owner, "First")
	f.joinSession(player, session)
	_, html := f.request(player, "GET", session.path+"?gm=true", "", http.StatusOK)
	if !strings.Contains(html, `data-gm="false"`) || strings.Contains(html, `id="gm-button"`) {
		t.Fatal("query flag elevated player role")
	}
	f.request(player, "POST", session.path+"board/clear?gm=true", "", http.StatusForbidden)
	f.request(owner, "POST", session.path+"board/clear", "", http.StatusNoContent)
}

func TestNonmembersCannotReadOrChangeBoardContent(t *testing.T) {
	f := newTableTestFixture(t)
	session := f.createSession(t, f.login("owner"), "First")
	stranger := f.login("stranger")
	f.request(stranger, "GET", session.path+"loadExistingUnits", "", http.StatusForbidden)
	f.request(stranger, "POST", session.path+"unit?unitID="+plugins.StableID("builtin", "unit", "knight")+"", "", http.StatusForbidden)
}

func TestSessionTokensAndArtworkAreIsolated(t *testing.T) {
	f := newTableTestFixture(t)
	owner := f.login("owner")
	first, second := f.createSession(t, owner, "First"), f.createSession(t, owner, "Second")
	f.request(owner, "POST", first.path+"unit?unitID="+plugins.StableID("builtin", "unit", "knight")+"", "", http.StatusOK)
	_, firstUnits := f.request(owner, "GET", first.path+"loadExistingUnits", "", http.StatusOK)
	_, secondUnits := f.request(owner, "GET", second.path+"loadExistingUnits", "", http.StatusOK)
	if !strings.Contains(firstUnits, `class="unit"`) || strings.Contains(secondUnits, `class="unit"`) {
		t.Fatal("tokens crossed session boundaries")
	}
	if !strings.Contains(firstUnits, `src="`+first.path+`images/knight"`) {
		t.Fatal("token artwork is not scoped to its session")
	}
	f.request(owner, "GET", first.path+"images/knight", "", http.StatusOK)
}

func TestDiceBroadcastsStayWithinSession(t *testing.T) {
	f := newTableTestFixture(t)
	owner := f.login("owner")
	first, second := f.createSession(t, owner, "First"), f.createSession(t, owner, "Second")
	firstSocket, secondSocket := f.connect(owner, first.path), f.connect(owner, second.path)
	for _, test := range []struct {
		session testSession
		socket  *websocket.Conn
		id      string
	}{{first, firstSocket, "first"}, {second, secondSocket, "second"}} {
		roll := fmt.Sprintf(`{"requestId":%q,"groups":[{"qty":1,"sides":6,"modifier":0}]}`, test.id)
		f.request(owner, "POST", test.session.path+"dice/roll", roll, http.StatusOK)
		var event map[string]any
		if err := test.socket.ReadJSON(&event); err != nil || event["type"] != "diceRolled" || event["requestId"] != test.id {
			t.Fatalf("wrong session broadcast: %v %v", event, err)
		}
	}
}

func TestPlayerSocketCannotPaintFogThroughGMQuery(t *testing.T) {
	f := newTableTestFixture(t)
	owner, player := f.login("owner"), f.login("player")
	session := f.createSession(t, owner, "First")
	f.joinSession(player, session)
	socket := f.connect(player, session.path)
	if err := socket.WriteJSON(map[string]any{"type": "fogMark", "id": "1", "mode": "paint", "width": 2, "points": []map[string]int{{"x": 1, "y": 1}}, "generation": 0}); err != nil {
		t.Fatal(err)
	}
	// Messages on this socket are processed in order. Receiving the allowed
	// ruler confirms the server has already handled the forbidden fog message.
	if err := socket.WriteJSON(map[string]any{"type": "rulerChanged", "start": map[string]int{"x": 1, "y": 1}, "end": map[string]int{"x": 2, "y": 2}, "generation": 0}); err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := socket.ReadJSON(&event); err != nil || event["type"] != "rulerChanged" {
		t.Fatalf("player socket changed fog: %v %v", event, err)
	}
}

func TestSessionOwnershipAndMembershipSurviveRestart(t *testing.T) {
	f := newTableTestFixture(t)
	owner, player := f.login("owner"), f.login("player")
	session := f.createSession(t, owner, "First")
	f.joinSession(player, session)
	reloaded, err := New(f.dir, f.assets)
	if err != nil {
		t.Fatal(err)
	}
	table, exists := reloaded.library.ByID(session.id)
	if !exists || table.OwnerID != "owner" || !table.HasMember("player") {
		t.Fatal("session membership did not persist")
	}
}

func TestLogoutRequiresSignInToReopenSession(t *testing.T) {
	f := newTableTestFixture(t)
	owner := f.login("owner")
	session := f.createSession(t, owner, "First")
	f.request(owner, "POST", "/auth/logout", "", http.StatusSeeOther)
	f.request(owner, "GET", session.path, "", http.StatusSeeOther)
}

type tableTestFixture struct {
	authentication *auth.Manager
	manager        *Manager
	dir, assets    string
	login          func(string) *http.Client
	request        func(*http.Client, string, string, string, int) (*http.Response, string)
	connect        func(*http.Client, string) *websocket.Conn
}

func newTableTestFixture(t *testing.T) tableTestFixture {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	assets := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	dir := t.TempDir()
	tables, err := New(dir, assets)
	if err != nil {
		t.Fatal(err)
	}
	authentication, err := auth.New(auth.Config{ClientID: "client", ClientSecret: "test-secret", RedirectURL: "http://localhost:18080/auth/discord/callback", HTTPClient: &http.Client{Transport: discordTransport{}, Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	tables.RegisterRoutes(mux)
	authentication.RegisterRoutes(mux)
	server := httptest.NewServer(authentication.Middleware(mux))
	t.Cleanup(server.Close)
	login := func(id string) *http.Client {
		t.Helper()
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Timeout: 5 * time.Second}
		response, err := client.Get(server.URL + "/auth/discord/login")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		target, _ := url.Parse(response.Header.Get("Location"))
		response, err = client.Get(server.URL + "/auth/discord/callback?code=" + id + "&state=" + target.Query().Get("state"))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 303 {
			t.Fatal("sign-in failed")
		}
		return client
	}
	request := func(client *http.Client, method, path, body string, status int) (*http.Response, string) {
		t.Helper()
		r, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != status {
			t.Fatalf("%s %s status %d, want %d: %s", method, path, response.StatusCode, status, data)
		}
		return response, string(data)
	}
	connect := func(client *http.Client, path string) *websocket.Conn {
		t.Helper()
		u, _ := url.Parse(server.URL)
		cookies := client.Jar.Cookies(u)
		var cookieHeader []string
		for _, cookie := range cookies {
			cookieHeader = append(cookieHeader, cookie.Name+"="+cookie.Value)
		}
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+path+"ws/updateUnitsForClients?gm=true", http.Header{"Cookie": {strings.Join(cookieHeader, "; ")}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var initial map[string]any
		if err := conn.ReadJSON(&initial); err != nil || initial["type"] != "rulerSession" {
			t.Fatalf("socket session: %v %v", initial, err)
		}
		return conn
	}
	return tableTestFixture{manager: tables, dir: dir, assets: assets, login: login, request: request, connect: connect, authentication: authentication}
}

func TestTabletopDeleteControlsAreLimitedToOwner(t *testing.T) {
	f := newTableTestFixture(t)
	owner, player, stranger := f.login("owner"), f.login("player"), f.login("stranger")
	first, second := f.createSession(t, owner, "First"), f.createSession(t, player, "Second")
	f.joinSession(player, first)
	_, html := f.request(owner, "GET", "/lobby", "", http.StatusOK)
	if !strings.Contains(html, first.path+"delete") || strings.Contains(html, second.path+"delete") {
		t.Fatal("delete links are not limited to owned tabletops")
	}
	f.request(player, "GET", first.path+"delete", "", http.StatusForbidden)
	f.request(player, "POST", first.path+"delete", "confirm=delete", http.StatusForbidden)
	f.request(stranger, "POST", first.path+"delete", "confirm=delete", http.StatusForbidden)
}

func TestTabletopDeletionRequiresConfirmation(t *testing.T) {
	f := newTableTestFixture(t)
	owner := f.login("owner")
	session := f.createSession(t, owner, "First")
	f.request(owner, "POST", session.path+"delete", "", http.StatusBadRequest)
	f.request(owner, "GET", session.path+"delete", "", http.StatusOK)
	if _, exists := f.manager.library.ByID(session.id); !exists {
		t.Fatal("confirmation page deleted tabletop")
	}
}

func TestTabletopDeletionRemovesFilesAndDisconnectsOnlyItsPlayers(t *testing.T) {
	f := newTableTestFixture(t)
	owner := f.login("owner")
	first, second := f.createSession(t, owner, "First"), f.createSession(t, owner, "Second")
	firstSocket, secondSocket := f.connect(owner, first.path), f.connect(owner, second.path)
	firstFile, secondFile := f.addArtwork(t, first), f.addArtwork(t, second)
	response, _ := f.request(owner, "POST", first.path+"delete", "confirm=delete", http.StatusSeeOther)
	if response.Header.Get("Location") != "/lobby" {
		t.Fatal("deletion did not return to lobby")
	}
	var event map[string]any
	if err := firstSocket.ReadJSON(&event); err == nil {
		t.Fatal("deleted tabletop kept socket connected")
	}
	if _, err := os.Stat(filepath.Dir(firstFile)); !os.IsNotExist(err) {
		t.Fatal("deleted tabletop retained files")
	}
	if _, err := os.Stat(secondFile); err != nil {
		t.Fatal("another tabletop lost artwork")
	}
	f.request(owner, "GET", first.path, "", http.StatusNotFound)
	f.request(owner, "GET", second.path, "", http.StatusOK)
	f.request(owner, "POST", second.path+"dice/roll", `{"requestId":"survivor","groups":[{"qty":1,"sides":6,"modifier":0}]}`, http.StatusOK)
	if err := secondSocket.ReadJSON(&event); err != nil || event["requestId"] != "survivor" {
		t.Fatal("another tabletop lost its socket")
	}
	reloaded, err := New(f.dir, f.assets)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := reloaded.library.ByID(first.id); exists {
		t.Fatal("deleted tabletop returned after restart")
	}
}

func TestPendingTabletopDeletionCanBeRetriedAfterRestart(t *testing.T) {
	f := newTableTestFixture(t)
	owner, player := f.login("owner"), f.login("player")
	session := f.createSession(t, owner, "First")
	f.joinSession(player, session)
	f.request(owner, "GET", session.path, "", http.StatusOK)
	artwork := f.addArtwork(t, session)
	cleanup := &cleanupFailureStore{err: errors.New("database unavailable")}
	f.manager.store = cleanup
	f.request(owner, "POST", session.path+"delete", "confirm=delete", http.StatusInternalServerError)
	f.request(owner, "GET", session.path, "", http.StatusNotFound)
	f.request(player, "POST", "/sessions/join", "code="+session.id, http.StatusBadRequest)
	_, html := f.request(owner, "GET", "/lobby", "", http.StatusOK)
	if !strings.Contains(html, "Retry deletion") {
		t.Fatal("owner has no cleanup retry action")
	}
	_, html = f.request(player, "GET", "/lobby", "", http.StatusOK)
	if strings.Contains(html, session.id) {
		t.Fatal("pending tabletop remains in player lobby")
	}
	if _, err := os.Stat(artwork); err != nil {
		t.Fatal("failed database cleanup removed artwork")
	}
	reloaded, err := NewWithStore(f.dir, f.assets, cleanup)
	if err != nil {
		t.Fatal(err)
	}
	if table, exists := reloaded.library.ByID(session.id); !exists || !table.Deleting {
		t.Fatal("deletion intent did not survive restart")
	}
	// Serve the reloaded manager with the existing signed-in user.
	mux := http.NewServeMux()
	reloaded.RegisterRoutes(mux)
	server := httptest.NewServer(f.authentication.Middleware(mux))
	defer server.Close()
	cleanup.err = nil
	response, err := owner.PostForm(server.URL+session.path+"delete", url.Values{"confirm": {"delete"}})
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("retry returned %d", response.StatusCode)
	}
	if _, exists := reloaded.library.ByID(session.id); exists {
		t.Fatal("retry did not finish deletion")
	}
	if len(cleanup.scopes) != 2 || cleanup.scopes[0] != session.id || cleanup.scopes[1] != session.id {
		t.Fatal("cleanup used the wrong scope")
	}
}

func (f tableTestFixture) addArtwork(t *testing.T, session testSession) string {
	t.Helper()
	path := filepath.Join(f.dir, "sessions", session.id, "artwork.png")
	if err := os.WriteFile(path, []byte("artwork"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
