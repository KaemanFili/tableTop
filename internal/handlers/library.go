package handlers

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"tableTop/main/internal/app"
)

const maxUnitImageBytes = 5 << 20

type spawnMenuData struct {
	Open     bool
	Creating bool
	PCs      []app.UnitDefinition
	NPCs     []app.UnitDefinition
	Name     string
	UnitType string
	Size     app.UnitSize
	Sizes    []app.UnitSizeOption
	ImageFit app.ImageFit
	Error    string
	Notice   string
}

func (h *EndpointHandler) menuData(open bool) spawnMenuData {
	data := spawnMenuData{Open: open}
	if open {
		for _, unit := range h.library.Snapshot() {
			if unit.UnitType == "pc" {
				data.PCs = append(data.PCs, unit)
			} else {
				data.NPCs = append(data.NPCs, unit)
			}
		}
	}
	return data
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
	h.renderSpawnMenu(w, spawnMenuData{Open: true, Creating: true, UnitType: "pc", Size: app.SizeMedium, ImageFit: app.DefaultImageFit()})
}

func (h *EndpointHandler) createUnit(w http.ResponseWriter, r *http.Request) {
	data := spawnMenuData{Open: true, Creating: true, UnitType: "pc", Size: app.SizeMedium, ImageFit: app.DefaultImageFit()}
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
	data.Name = strings.TrimSpace(r.FormValue("name"))
	data.UnitType = r.FormValue("unitType")
	data.Size = app.UnitSize(r.FormValue("size"))
	data.ImageFit, err = parseImageFit(r)
	if err != nil {
		fail(err.Error())
		return
	}
	if err := app.ValidateUnitDefinition(data.Name, data.UnitType, data.Size); err != nil {
		fail(err.Error())
		return
	}
	var pngImage []byte
	file, header, err := r.FormFile("image")
	if err != nil && !errors.Is(err, http.ErrMissingFile) {
		fail("The image could not be read. Please choose it again.")
		return
	}
	if err == nil {
		defer file.Close()
		if header.Size > maxUnitImageBytes {
			fail("The image must be 5 MB or smaller.")
			return
		}
		upload, err := io.ReadAll(io.LimitReader(file, maxUnitImageBytes+1))
		if err != nil || len(upload) > maxUnitImageBytes {
			fail("The image could not be read or is larger than 5 MB.")
			return
		}
		pngImage, err = normalizeUnitImage(upload)
		if err != nil {
			fail(err.Error())
			return
		}
	}
	unit, err := h.library.Create(data.Name, data.UnitType, data.Size, data.ImageFit, pngImage)
	if err != nil {
		log.Println("Save unit:", err)
		fail("The unit could not be saved. Please try again. If you selected an image, choose it again before saving.")
		return
	}
	menu := h.menuData(true)
	menu.Notice = unit.Name + " saved. Select it below to spawn a copy."
	h.renderSpawnMenu(w, menu)
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

func normalizeUnitImage(upload []byte) ([]byte, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(upload))
	if err != nil {
		return nil, errors.New("Choose a valid PNG, JPEG, or GIF image.")
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 {
		return nil, errors.New("Image dimensions must be 4096 × 4096 pixels or smaller.")
	}
	decoded, _, err := image.Decode(bytes.NewReader(upload))
	if err != nil {
		return nil, errors.New("This image is damaged or incomplete. Please choose another image.")
	}
	var output bytes.Buffer
	if err := png.Encode(&output, decoded); err != nil {
		return nil, errors.New("The image could not be processed. Please choose another image.")
	}
	return output.Bytes(), nil
}
