package handlers

import (
	"bytes"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tableTop/main/internal/app"
)

func libraryRequest(t *testing.T, name, unitType, size string, upload []byte, extraFields ...map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{"name": name, "unitType": unitType, "size": size} {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	for _, fields := range extraFields {
		for key, value := range fields {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	if upload != nil {
		file, err := writer.CreateFormFile("image", "../../portrait.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(upload); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/unit-library", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	return r
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return imageData.Bytes()
}

func TestUnitLibraryCreateRestartAndSpawn(t *testing.T) {
	t.Chdir("../..")
	dir := t.TempDir()
	library, err := app.NewUnitLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	state := app.NewState()
	mux := http.NewServeMux()
	NewEndpointHandler(state, NewWebSocketHandler(state), library).RegisterRoutes(mux)
	for _, unitType := range []string{"pc", "npc"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, libraryRequest(t, "  Scout <script>  ", unitType, "large", testPNG(t, 2, 2), map[string]string{"imageZoom": "2", "imageOffsetX": "10", "imageOffsetY": "-20"}))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "saved. Select it below") {
			t.Fatalf("create response = %d %s", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "<script>") || !strings.Contains(response.Body.String(), "&lt;script&gt;") {
			t.Fatal("unit name must be escaped in HTML")
		}
	}
	if state.UnitCount() != 0 {
		t.Fatal("saving to the library must not automatically spawn a token")
	}

	// Reopen storage and use new handlers, as on a server restart.
	library, err = app.NewUnitLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	mux = http.NewServeMux()
	NewEndpointHandler(state, NewWebSocketHandler(state), library).RegisterRoutes(mux)
	if len(library.Snapshot()) != 4 {
		t.Fatal("custom entries were not restored")
	}
	for _, unit := range library.Snapshot() {
		if unit.ID == "knight" || unit.ID == "goblin" {
			continue
		}
		if unit.Name != "Scout <script>" || unit.Size != app.SizeLarge || unit.ImageFit != (app.ImageFit{Zoom: 2, OffsetX: 10, OffsetY: -20}) {
			t.Fatalf("unexpected saved name %q", unit.Name)
		}
		artwork := httptest.NewRecorder()
		mux.ServeHTTP(artwork, httptest.NewRequest(http.MethodGet, "/images/"+unit.ImageID, nil))
		if artwork.Code != http.StatusOK || artwork.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("saved image response: %d", artwork.Code)
		}
		if _, err := png.Decode(artwork.Body); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			spawn := httptest.NewRecorder()
			mux.ServeHTTP(spawn, httptest.NewRequest(http.MethodPost, "/unit?unitID="+unit.ID, nil))
			if spawn.Code != http.StatusOK || !strings.Contains(spawn.Body.String(), `src="/images/`+unit.ImageID+`"`) || !strings.Contains(spawn.Body.String(), "Scout &lt;script&gt;") || !strings.Contains(spawn.Body.String(), "--image-zoom: 2; --image-offset-x: 10%; --image-offset-y: -20%;") {
				t.Fatalf("spawn response: %d %s", spawn.Code, spawn.Body.String())
			}
		}
	}
	if state.UnitCount() != 4 {
		t.Fatal("library entries should spawn independent copies")
	}
	for _, unit := range state.UnitsSnapshot() {
		if unit.ImageFit != (app.ImageFit{Zoom: 2, OffsetX: 10, OffsetY: -20}) || unit.Size != app.SizeLarge || unit.Name != "Scout <script>" || (unit.UnitType != "pc" && unit.UnitType != "npc") {
			t.Fatalf("spawn lost identity: %+v", unit)
		}
	}
	missing := httptest.NewRecorder()
	mux.ServeHTTP(missing, httptest.NewRequest(http.MethodPost, "/unit?unitID=missing", nil))
	if missing.Code != http.StatusNotFound || state.UnitCount() != 4 {
		t.Fatal("unknown library units must not spawn")
	}
}

func TestCreateUnitValidation(t *testing.T) {
	t.Chdir("../..")
	for _, test := range []struct {
		label, name, unitType, message string
		image                          []byte
	}{
		{label: "empty name", name: " ", unitType: "pc", message: "Enter a name"},
		{label: "long name", name: strings.Repeat("x", 81), unitType: "pc", message: "Enter a name"},
		{label: "invalid type", name: "Scout", unitType: "other", message: "Choose PC or NPC"},
		{label: "not an image", name: "Scout", unitType: "npc", image: []byte("<svg onload='alert(1)'></svg>"), message: "Choose a valid"},
		{label: "too large", name: "Scout", unitType: "npc", image: make([]byte, maxUnitImageBytes+1), message: "5 MB"},
		{label: "too wide", name: "Scout", unitType: "pc", image: testPNG(t, 4097, 1), message: "4096"},
	} {
		t.Run(test.label, func(t *testing.T) {
			library := newTestLibrary(t)
			state := app.NewState()
			mux := http.NewServeMux()
			NewEndpointHandler(state, NewWebSocketHandler(state), library).RegisterRoutes(mux)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, libraryRequest(t, test.name, test.unitType, "huge", test.image))
			body := response.Body.String()
			if !strings.Contains(body, `role="alert"`) || !strings.Contains(body, test.message) || !strings.Contains(body, `id="create-unit-form"`) {
				t.Fatalf("expected actionable form error, got %s", body)
			}
			if !strings.Contains(body, `value="huge" selected`) {
				t.Fatal("form error did not preserve the selected size")
			}
			if len(library.Snapshot()) != 2 || state.UnitCount() != 0 {
				t.Fatal("invalid creation changed saved units")
			}
		})
	}
}

func TestUnitMenusAndDefaultArtwork(t *testing.T) {
	t.Chdir("../..")
	library := newTestLibrary(t)
	state := app.NewState()
	mux := http.NewServeMux()
	NewEndpointHandler(state, NewWebSocketHandler(state), library).RegisterRoutes(mux)
	for _, test := range []struct {
		path string
		want []string
	}{
		{path: "/spawn-menu?open=true", want: []string{`<details`, `id="pc-list"`, `id="npc-list"`, "Knight", "Goblin", "/images/knight", "/images/goblin", "Create new unit"}},
		{path: "/unit-library/new", want: []string{`name="name"`, `name="unitType"`, `name="size"`, `value="medium" selected`, `id="token-image-preview"`, `name="imageZoom"`, `name="imageOffsetX"`, `name="imageOffsetY"`, `type="file"`, `multipart/form-data`, "Save unit", "Cancel"}},
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("menu response = %d", response.Code)
		}
		for _, want := range test.want {
			if !strings.Contains(response.Body.String(), want) {
				t.Fatalf("%s is missing %q", test.path, want)
			}
		}
	}
	for _, unitType := range []string{"pc", "npc"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, libraryRequest(t, "Default "+unitType, unitType, "medium", nil))
		if !strings.Contains(response.Body.String(), "saved. Select it below") {
			t.Fatal("creating without an upload failed")
		}
	}
	for _, unit := range library.Snapshot() {
		want := app.NPCUnitImageID
		if unit.UnitType == "pc" {
			want = app.PCUnitImageID
		}
		if unit.ImageID != want {
			t.Fatalf("wrong default artwork: %+v", unit)
		}
	}
}

func TestSizeControlsSpawnAndReloadFootprint(t *testing.T) {
	t.Chdir("../..")
	library := newTestLibrary(t)
	state := app.NewState()
	mux := http.NewServeMux()
	NewEndpointHandler(state, NewWebSocketHandler(state), library).RegisterRoutes(mux)
	for _, test := range []struct{ size, squares string }{
		{"tiny", "0.5"}, {"small", "1"}, {"medium", "1"},
		{"large", "2"}, {"huge", "3"}, {"gargantuan", "4"},
	} {
		t.Run(test.size, func(t *testing.T) {
			created := httptest.NewRecorder()
			mux.ServeHTTP(created, libraryRequest(t, "Size "+test.size, "npc", test.size, nil))
			if !strings.Contains(created.Body.String(), "saved. Select it below") {
				t.Fatalf("creation failed: %s", created.Body.String())
			}
			var definition app.UnitDefinition
			for _, unit := range library.Snapshot() {
				if unit.Name == "Size "+test.size {
					definition = unit
				}
			}
			if string(definition.Size) != test.size {
				t.Fatal("creation did not save the selected size")
			}
			spawn := httptest.NewRecorder()
			mux.ServeHTTP(spawn, httptest.NewRequest(http.MethodPost, "/unit?unitID="+definition.ID, nil))
			footprint := "--unit-squares: " + test.squares + ";"
			if spawn.Code != http.StatusOK || !strings.Contains(spawn.Body.String(), footprint) || !strings.Contains(spawn.Body.String(), `data-size="`+test.size+`"`) {
				t.Fatalf("incorrect token footprint: %s", spawn.Body.String())
			}
			for _, unit := range state.UnitsSnapshot() {
				if unit.Name == definition.Name {
					state.UpdateUnitPosition(unit.ID, 400, 500)
					moved, _ := state.UnitByID(unit.ID)
					if moved.Size != definition.Size {
						t.Fatal("movement lost size")
					}
				}
			}
			reload := httptest.NewRecorder()
			mux.ServeHTTP(reload, httptest.NewRequest(http.MethodGet, "/loadExistingUnits", nil))
			if !strings.Contains(reload.Body.String(), "left: 400px; top: 500px; "+footprint) {
				t.Fatal("reloaded token lost its footprint")
			}
		})
	}
}

func TestRejectInvalidUnitSizes(t *testing.T) {
	t.Chdir("../..")
	library := newTestLibrary(t)
	state := app.NewState()
	mux := http.NewServeMux()
	NewEndpointHandler(state, NewWebSocketHandler(state), library).RegisterRoutes(mux)
	for _, size := range []string{"", "gigantic", "2", "large; color:red"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, libraryRequest(t, "Scout", "npc", size, nil))
		if !strings.Contains(response.Body.String(), "Choose a size from Tiny to Gargantuan") || len(library.Snapshot()) != 2 {
			t.Fatalf("invalid size %q was not rejected", size)
		}
		if _, err := library.Create("Scout", "npc", app.UnitSize(size), app.DefaultImageFit(), nil); err == nil {
			t.Fatalf("library accepted invalid size %q", size)
		}
	}
}

func TestRejectInvalidImageFraming(t *testing.T) {
	t.Chdir("../..")
	library := newTestLibrary(t)
	state := app.NewState()
	mux := http.NewServeMux()
	NewEndpointHandler(state, NewWebSocketHandler(state), library).RegisterRoutes(mux)
	for _, fields := range []map[string]string{
		{"imageZoom": "NaN"}, {"imageZoom": "+Inf"}, {"imageZoom": "0"},
		{"imageZoom": "4.1"}, {"imageZoom": "invalid"},
		{"imageOffsetX": "51"}, {"imageOffsetY": "-51"}, {"imageOffsetY": "NaN"},
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, libraryRequest(t, "Scout", "npc", "large", nil, fields))
		if !strings.Contains(response.Body.String(), `role="alert"`) || len(library.Snapshot()) != 2 {
			t.Fatalf("invalid framing was not rejected: %v", fields)
		}
	}
}

func TestFormErrorsPreserveImageFraming(t *testing.T) {
	t.Chdir("../..")
	state := app.NewState()
	mux := http.NewServeMux()
	NewEndpointHandler(state, NewWebSocketHandler(state), newTestLibrary(t)).RegisterRoutes(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, libraryRequest(t, "Scout", "npc", "huge", []byte("broken image"), map[string]string{
		"imageZoom": "1.75", "imageOffsetX": "12.5", "imageOffsetY": "-25",
	}))
	body := response.Body.String()
	for _, expected := range []string{`role="alert"`, `value="1.75"`, `value="12.5"`, `value="-25"`, `data-preserve-fit="true"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("form error lost image framing: missing %s", expected)
		}
	}
}
