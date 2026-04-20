package videochannel

import (
	"encoding/base64"
	"fmt"
	"image"
	"strings"

	barcodedm "github.com/boombuler/barcode/datamatrix"
	"github.com/makiuchi-d/gozxing"
	zxingdm "github.com/makiuchi-d/gozxing/datamatrix"
)

const (
	quietZone = 10
)

func renderVisualFrame(payload []byte, width, height int) ([]byte, error) {
	logicalFrameBytes := width * height
	frame := make([]byte, logicalFrameBytes)
	for i := range frame {
		frame[i] = 0xff // White background
	}

	if len(payload) == 0 {
		return frame, nil
	}

	encoded := base64.StdEncoding.EncodeToString(payload)
	dm, err := barcodedm.Encode(encoded)
	if err != nil {
		return nil, fmt.Errorf("datamatrix encode: %w", err)
	}

	// Use strict integer scaling to keep edges sharp
	bounds := dm.Bounds()
	dmW := bounds.Dx()
	dmH := bounds.Dy()

	scaleW := (width - (quietZone * 2)) / dmW
	scaleH := (height - (quietZone * 2)) / dmH
	scale := scaleW
	if scaleH < scale {
		scale = scaleH
	}
	if scale < 1 {
		scale = 1
	}

	totalW := dmW * scale
	totalH := dmH * scale
	offsetX := (width - totalW) / 2
	offsetY := (height - totalH) / 2

	for y := 0; y < dmH; y++ {
		for x := 0; x < dmW; x++ {
			r, _, _, _ := dm.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			if r < 0x8000 {
				// Fill scale x scale block
				for sy := 0; sy < scale; sy++ {
					for sx := 0; sx < scale; sx++ {
						pixelX := offsetX + (x * scale) + sx
						pixelY := offsetY + (y * scale) + sy
						if pixelX < width && pixelY < height {
							frame[pixelY*width+pixelX] = 0x00
						}
					}
				}
			}
		}
	}

	return frame, nil
}

func extractVisualPayload(frame []byte, width, height int) ([]byte, error) {
	logicalFrameBytes := width * height
	if len(frame) != logicalFrameBytes {
		return nil, fmt.Errorf("unexpected frame size: %d (expected %dx%d=%d)", len(frame), width, height, logicalFrameBytes)
	}

	img := image.NewGray(image.Rect(0, 0, width, height))
	copy(img.Pix, frame)

	source := gozxing.NewLuminanceSourceFromImage(img)
	// HybridBinarizer is good for noisy images
	binarizer := gozxing.NewHybridBinarizer(source)
	bmp, err := gozxing.NewBinaryBitmap(binarizer)
	if err != nil {
		return nil, fmt.Errorf("bitmap: %w", err)
	}

	reader := zxingdm.NewDataMatrixReader()
	hints := make(map[gozxing.DecodeHintType]interface{})
	hints[gozxing.DecodeHintType_TRY_HARDER] = true
	hints[gozxing.DecodeHintType_PURE_BARCODE] = true

	result, err := reader.Decode(bmp, hints)
	if err != nil {
		if strings.Contains(err.Error(), "NotFoundException") {
			return nil, nil
		}
		return nil, fmt.Errorf("decode: %w", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(result.GetText())
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}

	return decoded, nil
}
