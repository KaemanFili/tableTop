package artwork

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
)

func Normalize(upload []byte) ([]byte, error) {
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
