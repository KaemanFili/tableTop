package tables

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"tableTop/main/internal/auth"
)

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

func TestCreateJoinOwnershipAndBoardIsolation(t *testing.T) {
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
	defer server.Close()
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
	owner, player, stranger := login("owner"), login("player"), login("stranger")
	response, _ := request(owner, "POST", "/sessions", "name=First", 303)
	first := response.Header.Get("Location")
	firstID := strings.Split(first, "/")[2]
	_, html := request(owner, "GET", first, "", 200)
	if !strings.Contains(html, `data-gm="true"`) || !strings.Contains(html, `hx-get="`+first+`loadExistingUnits"`) || !strings.Contains(html, `data-session-prefix="`+strings.TrimSuffix(first, "/")+`"`) {
		t.Fatal("creator board lacks its role or request prefix")
	}
	request(player, "POST", first+"board/clear?gm=true", "", 403)
	response, _ = request(player, "GET", first, "", 303)
	if !strings.Contains(response.Header.Get("Location"), "invite="+firstID) {
		t.Fatal("nonmember was not offered a join form")
	}
	request(player, "POST", "/sessions/join", "code="+firstID, 303)
	_, html = request(player, "GET", first+"?gm=true", "", 200)
	if !strings.Contains(html, `data-gm="false"`) || strings.Contains(html, `id="gm-button"`) {
		t.Fatal("joining player elevated role through query")
	}
	request(player, "POST", first+"board/clear?gm=true", "", 403)
	request(owner, "POST", first+"board/clear", "", 204)
	response, _ = request(player, "POST", "/sessions", "name=Second", 303)
	second := response.Header.Get("Location")
	_, html = request(player, "GET", second, "", 200)
	if !strings.Contains(html, `data-gm="true"`) {
		t.Fatal("player is not GM of their own session")
	}
	request(stranger, "GET", first+"loadExistingUnits", "", 403)
	request(stranger, "POST", first+"unit?unitID=knight", "", 403)
	request(owner, "POST", first+"unit?unitID=knight", "", 200)
	_, firstUnits := request(player, "GET", first+"loadExistingUnits", "", 200)
	_, secondUnits := request(player, "GET", second+"loadExistingUnits", "", 200)
	if !strings.Contains(firstUnits, `class="unit"`) || strings.Contains(secondUnits, `class="unit"`) {
		t.Fatal("units crossed session boundaries")
	}
	if !strings.Contains(firstUnits, `src="`+first+`images/knight"`) {
		t.Fatal("token artwork is not scoped to its session")
	}
	request(player, "GET", first+"images/knight", "", 200)
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
	sameBoard := connect(player, first)
	otherBoard := connect(player, second)
	// A player's gm=true socket cannot paint fog. Its next received message
	// must be the valid shared dice roll from that same table.
	sameBoard.WriteJSON(map[string]any{"type": "fogMark", "x": 1, "y": 1, "radius": 1, "generation": 1})
	roll := `{"requestId":"first","groups":[{"qty":2,"sides":6,"modifier":3}]}`
	request(owner, "POST", first+"dice/roll", roll, 200)
	var event map[string]any
	if err := sameBoard.ReadJSON(&event); err != nil || event["type"] != "diceRolled" || event["requestId"] != "first" {
		t.Fatalf("role check or same-table broadcast failed: %v %v", event, err)
	}
	roll = strings.Replace(roll, "first", "second", 1)
	request(player, "POST", second+"dice/roll", roll, 200)
	if err := otherBoard.ReadJSON(&event); err != nil || event["requestId"] != "second" {
		t.Fatalf("roll leaked between tables: %v %v", event, err)
	}
	reloaded, err := New(dir, assets)
	if err != nil {
		t.Fatal(err)
	}
	table, ok := reloaded.library.ByID(firstID)
	if !ok || table.OwnerID != "owner" || !table.HasMember("player") {
		t.Fatal("session ownership or membership did not persist")
	}
	request(owner, "POST", "/auth/logout", "", 303)
	request(owner, "GET", first, "", 303)
}
