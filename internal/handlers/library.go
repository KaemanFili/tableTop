package handlers

import (
	"bytes"
	"errors"
	"golang.org/x/text/unicode/norm"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"tableTop/main/internal/app"
	"tableTop/main/internal/artwork"
)

const maxUnitImageBytes = 5 << 20

type libraryGroup[T any] struct {
	Owned          []T
	Defaults       []T
	OwnedMatches   int
	DefaultMatches int
	Expanded       bool
}

func (group libraryGroup[T]) OwnedTotal() int { return max(len(group.Owned), group.OwnedMatches) }
func (group libraryGroup[T]) DefaultTotal() int {
	return max(len(group.Defaults), group.DefaultMatches)
}
func (group libraryGroup[T]) Total() int { return group.OwnedTotal() + group.DefaultTotal() }

type spawnMenuData struct {
	Open         bool
	Creating     bool
	PCs          libraryGroup[app.UnitDefinition]
	NPCs         libraryGroup[app.UnitDefinition]
	CopyID       string
	Name         string
	UnitType     string
	Size         app.UnitSize
	Sizes        []app.UnitSizeOption
	ImageFit     app.ImageFit
	Error        string
	Notice       string
	ServerSearch bool
	Search       string
	Shown        int
	Page         int
}

func (data spawnMenuData) MatchCount() int { return data.PCs.Total() + data.NPCs.Total() }

const unitMenuGroupLimit = 60

func (h *EndpointHandler) menuData(open bool) spawnMenuData {
	return h.searchMenuData(open, "")
}

// Large catalogs use bounded server results instead of thousands of HTMX controls.
func (h *EndpointHandler) searchMenuData(open bool, query string) spawnMenuData {
	return h.searchMenuPageData(open, query, 0)
}

func (data spawnMenuData) NextPage() int     { return data.Page + 1 }
func (data spawnMenuData) PreviousPage() int { return max(0, data.Page-1) }
func (data spawnMenuData) HasNextPage() bool {
	limit := (data.Page + 1) * unitMenuGroupLimit
	return data.PCs.OwnedTotal() > limit || data.PCs.DefaultTotal() > limit || data.NPCs.OwnedTotal() > limit || data.NPCs.DefaultTotal() > limit
}

func (h *EndpointHandler) searchMenuPageData(open bool, query string, page int) spawnMenuData {
	data := spawnMenuData{Open: open, Search: query, Page: max(0, min(page, 10000))}
	offset := data.Page * unitMenuGroupLimit
	if !open {
		return data
	}
	units := h.library.Units()
	data.ServerSearch = len(units) > 4*unitMenuGroupLimit
	terms := strings.Fields(normalizeLibrarySearch(query))
	for _, unit := range units {
		matches := true
		name := normalizeLibrarySearch(unit.Name)
		for _, term := range terms {
			if !strings.Contains(name, term) {
				matches = false
				break
			}
		}
		if data.ServerSearch && !matches {
			continue
		}
		group := &data.NPCs
		if unit.UnitType == "pc" {
			group = &data.PCs
		}
		group.Expanded = query != "" || data.Page > 0
		if unit.ReadOnly {
			group.DefaultMatches++
			if !data.ServerSearch || (group.DefaultMatches > offset && group.DefaultMatches <= offset+unitMenuGroupLimit) {
				group.Defaults = append(group.Defaults, unit)
				data.Shown++
			}
		} else {
			group.OwnedMatches++
			if !data.ServerSearch || (group.OwnedMatches > offset && group.OwnedMatches <= offset+unitMenuGroupLimit) {
				group.Owned = append(group.Owned, unit)
				data.Shown++
			}
		}
	}
	return data
}

// Match the browser search's case and accent handling.
func normalizeLibrarySearch(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return unicode.ToLower(r)
	}, norm.NFKD.String(value))
}

func (h *EndpointHandler) renderSpawnMenu(w http.ResponseWriter, data spawnMenuData) {
	data.Sizes = app.UnitSizes()
	w.Header().Set("Cache-Control", "no-store")
	var rendered bytes.Buffer
	if err := h.indexTemplate.ExecuteTemplate(&rendered, "spawn-controls", data); err != nil {
		log.Println("Render unit menu:", err)
		http.Error(w, "The unit menu could not be rendered. Restart the server after updating the app.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(rendered.Bytes())
}

func (h *EndpointHandler) newUnitForm(w http.ResponseWriter, r *http.Request) {
	data := spawnMenuData{Open: true, Creating: true, UnitType: "pc", Size: app.SizeMedium, ImageFit: app.DefaultImageFit()}
	if id := r.URL.Query().Get("copy"); id != "" {
		u, ok := h.library.UnitByID(id)
		if !ok || !u.ReadOnly {
			http.NotFound(w, r)
			return
		}
		data.CopyID = id
		data.Name = u.Name
		data.UnitType = u.UnitType
		data.Size = u.Size
		data.ImageFit = u.ImageFit
	}
	h.renderSpawnMenu(w, data)
}

func (h *EndpointHandler) createUnit(w http.ResponseWriter, r *http.Request) {
	data := spawnMenuData{Open: true, Creating: true, UnitType: "pc", Size: app.SizeMedium, ImageFit: app.DefaultImageFit(), CopyID: r.PathValue("id")}
	// Return a rendered form on validation failures so HTMX displays the error.
	fail := func(message string) {
		data.Error = message
		h.renderSpawnMenu(w, data)
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUnitImageBytes+(64<<10))
	err := r.ParseMultipartForm(maxUnitImageBytes + (64 << 10))
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if err != nil {
		fail("The upload could not be read. Choose a PNG, JPEG, or GIF image up to 5 MB.")
		return
	}
	if err := parseUnitForm(r, &data); err != nil {
		fail(err.Error())
		return
	}
	pngImage, err := readUnitImage(r)
	if err != nil {
		fail(err.Error())
		return
	}
	unit, err := h.saveLibraryUnit(data, pngImage)
	if err != nil {
		log.Println("Save unit:", err)
		if errors.Is(err, app.ErrStorageLimit) {
			fail(err.Error())
			return
		}
		fail("The unit could not be saved. Please try again. If you selected an image, choose it again before saving.")
		return
	}
	menu := h.menuData(true)
	menu.Notice = unit.Name + " saved. Select it below to spawn a copy."
	h.renderSpawnMenu(w, menu)
}

// Update form data before validation so errors preserve the submitted fields.
func parseUnitForm(r *http.Request, data *spawnMenuData) error {
	data.Name = strings.TrimSpace(r.FormValue("name"))
	data.UnitType = r.FormValue("unitType")
	data.Size = app.UnitSize(r.FormValue("size"))
	var err error
	data.ImageFit, err = parseImageFit(r)
	if err != nil {
		return err
	}
	return app.ValidateUnitDefinition(data.Name, data.UnitType, data.Size)
}

func readUnitImage(r *http.Request) ([]byte, error) {
	file, header, err := r.FormFile("image")
	if errors.Is(err, http.ErrMissingFile) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("The image could not be read. Please choose it again.")
	}
	defer file.Close()
	if header.Size > maxUnitImageBytes {
		return nil, errors.New("The image must be 5 MB or smaller.")
	}
	upload, err := io.ReadAll(io.LimitReader(file, maxUnitImageBytes+1))
	if err != nil || len(upload) > maxUnitImageBytes {
		return nil, errors.New("The image could not be read or is larger than 5 MB.")
	}
	return normalizeUnitImage(upload)
}

func (h *EndpointHandler) saveLibraryUnit(data spawnMenuData, pngImage []byte) (app.UnitDefinition, error) {
	if data.CopyID != "" {
		return h.library.CreateCopy(data.CopyID, data.Name, data.UnitType, data.Size, data.ImageFit, pngImage)
	}
	return h.library.Create(data.Name, data.UnitType, data.Size, data.ImageFit, pngImage)
}

func parseImageFit(r *http.Request) (app.ImageFit, error) {
	fit := app.DefaultImageFit()
	for name, value := range map[string]*float64{
		"imageZoom": &fit.Zoom, "imageOffsetX": &fit.OffsetX, "imageOffsetY": &fit.OffsetY,
	} {
		if input := r.FormValue(name); input != "" {
			parsed, err := strconv.ParseFloat(input, 64)
			if err != nil {
				return app.DefaultImageFit(), errors.New("The image framing is invalid. Adjust the preview and try again.")
			}
			*value = parsed
		}
	}
	if err := fit.Validate(); err != nil {
		return app.DefaultImageFit(), err
	}
	return fit, nil
}

func normalizeUnitImage(upload []byte) ([]byte, error) { return artwork.Normalize(upload) }

func (h *EndpointHandler) deleteLibraryUnit(w http.ResponseWriter, r *http.Request) {
	menu := h.menuData(true)
	if err := h.library.Delete(r.PathValue("id")); err != nil {
		if errors.Is(err, app.ErrReadOnly) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		log.Println("Delete unit:", err)
		menu.Error = "The unit could not be deleted. Please try again."
	} else {
		menu = h.menuData(true)
		menu.Notice = "Unit permanently deleted from the library."
	}
	h.renderSpawnMenu(w, menu)
}
