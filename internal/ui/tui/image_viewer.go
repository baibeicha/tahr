package tui

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/media"
	"tahr/internal/ui"
)

// Supported image extensions
var imageExtensions = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".bmp":  true,
	".webp": true,
	".ico":  true,
}

// IsImageFile returns true if the filePath corresponds to an image file.
func IsImageFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return imageExtensions[ext]
}

// ImageViewerState manages the lifecycle, caching, metadata, and rendering of an image inside an editor pane.
type ImageViewerState struct {
	mu            sync.RWMutex
	FilePath      string
	Image         image.Image
	Width         int
	Height        int
	Format        string
	FileSize      int64
	ScaleMode     media.ScaleMode
	Protocol      media.Protocol
	IsAnimated    bool
	GifPlayer     *media.VideoPlayerWidget
	LastFrameTime time.Time
	Err           error
}

// NewImageViewerState loads an image file from disk and initializes its viewer state.
func NewImageViewerState(filePath string) *ImageViewerState {
	iv := &ImageViewerState{
		FilePath:      filePath,
		ScaleMode:     media.ScaleFit,
		Protocol:      media.DetectProtocol(),
		LastFrameTime: time.Now(),
	}

	fi, err := os.Stat(filePath)
	if err != nil {
		iv.Err = err
		return iv
	}
	iv.FileSize = fi.Size()

	f, err := os.Open(filePath)
	if err != nil {
		iv.Err = err
		return iv
	}
	defer f.Close()

	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".gif" {
		// Attempt to load animated GIF via VideoPlayer
		player := media.NewVideoPlayer()
		if err := player.LoadGIF(filePath); err == nil && player.TotalFrames() > 1 {
			iv.IsAnimated = true
			iv.GifPlayer = player
			iv.Format = "GIF (Animated)"
			// Also decode first frame for fallback dimensions
			_, _ = f.Seek(0, 0)
			img, _, err := image.Decode(f)
			if err == nil {
				iv.Image = img
				iv.Width = img.Bounds().Dx()
				iv.Height = img.Bounds().Dy()
			}
			return iv
		}
		// Fallback to static GIF if not animated or player error
		_, _ = f.Seek(0, 0)
	}

	img, format, err := image.Decode(f)
	if err != nil {
		iv.Err = fmt.Errorf("decode %s: %w", filepath.Base(filePath), err)
		return iv
	}

	iv.Image = img
	iv.Width = img.Bounds().Dx()
	iv.Height = img.Bounds().Dy()
	iv.Format = strings.ToUpper(format)
	return iv
}

// CycleScaleMode cycles between Fit -> Fill -> Stretch.
func (iv *ImageViewerState) CycleScaleMode() media.ScaleMode {
	iv.mu.Lock()
	defer iv.mu.Unlock()
	switch iv.ScaleMode {
	case media.ScaleFit:
		iv.ScaleMode = media.ScaleFill
	case media.ScaleFill:
		iv.ScaleMode = media.ScaleStretch
	default:
		iv.ScaleMode = media.ScaleFit
	}
	return iv.ScaleMode
}

// ScaleModeTitle returns human readable mode label.
func (iv *ImageViewerState) ScaleModeTitle() string {
	iv.mu.RLock()
	defer iv.mu.RUnlock()
	switch iv.ScaleMode {
	case media.ScaleFit:
		return "Fit"
	case media.ScaleFill:
		return "Fill"
	case media.ScaleStretch:
		return "Stretch"
	default:
		return "Fit"
	}
}

// Render renders the image viewer inside the designated pane area.
func (iv *ImageViewerState) Render(buf *buffer.Buffer, area buffer.Rect, theme *ui.Theme) {
	if buf == nil || area.IsEmpty() {
		return
	}

	bg := toColor(theme.Background)
	headerBg := toColor(theme.GutterBg)
	headerFg := toColor(theme.Foreground)
	subFg := toColor(theme.Comment)
	accentFg := toColor(theme.Function)

	// Clear area
	for y := area.Y; y < area.Y+area.Height; y++ {
		for x := area.X; x < area.X+area.Width; x++ {
			buf.Set(x, y, cell.Cell{
				Rune:     ' ',
				Width:    1,
				FgType:   cell.ColorDefault,
				BgType:   cell.ColorRGB,
				Bg:       bg.Value,
				Modifier: cell.AttrNone,
			})
		}
	}

	// 1. Top metadata bar (1 row)
	headerRow := area.Y
	for x := area.X; x < area.X+area.Width; x++ {
		buf.Set(x, headerRow, cell.Cell{
			Rune:     ' ',
			Width:    1,
			FgType:   headerFg.Type,
			Fg:       headerFg.Value,
			BgType:   headerBg.Type,
			Bg:       headerBg.Value,
			Modifier: cell.AttrNone,
		})
	}

	// Format file size
	sizeStr := formatFileSize(iv.FileSize)
	dimStr := fmt.Sprintf("%dx%d px", iv.Width, iv.Height)
	if iv.Width == 0 || iv.Height == 0 {
		dimStr = "unknown size"
	}

	protoStr := iv.Protocol.String()
	modeStr := fmt.Sprintf("Scale: %s (M)", iv.ScaleModeTitle())
	infoText := fmt.Sprintf(" ● %s  │  %s  │  %s  │  %s  │  %s  │  %s ",
		filepath.Base(iv.FilePath), dimStr, sizeStr, iv.Format, protoStr, modeStr)

	curX := area.X
	for _, r := range infoText {
		if curX >= area.X+area.Width {
			break
		}
		fg := headerFg
		if strings.ContainsRune("●│()", r) {
			fg = subFg
		} else if strings.ContainsRune("MFitFillStretch", r) {
			fg = accentFg
		}
		buf.SetRune(curX, headerRow, r, fg, headerBg, cell.AttrNone)
		curX++
	}

	// 2. Image Render Area
	if area.Height <= 2 {
		return
	}
	renderArea := buffer.NewRect(area.X, area.Y+1, area.Width, area.Height-1)

	if iv.Err != nil {
		errText := fmt.Sprintf("Unable to display image: %v", iv.Err)
		errX := area.X + max(1, (area.Width-len(errText))/2)
		errY := area.Y + area.Height/2
		buf.SetString(errX, errY, errText, toColor(theme.DiagnosticError), bg, cell.AttrBold)
		return
	}

	if iv.IsAnimated && iv.GifPlayer != nil {
		now := time.Now()
		elapsed := now.Sub(iv.LastFrameTime)
		iv.LastFrameTime = now
		iv.GifPlayer.Advance(elapsed)
		iv.GifPlayer.Draw(buf, renderArea)
		return
	}

	if iv.Image != nil {
		if iv.Protocol == media.ProtoBraille {
			br := media.NewBrailleRenderer()
			br.Dithering = true
			br.DrawImage(buf, renderArea, iv.Image)
		} else {
			media.RenderHalfBlock(buf, renderArea, iv.Image, iv.ScaleMode)
		}
	}
}

func formatFileSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024.0)
	}
	return fmt.Sprintf("%.2f MB", float64(bytes)/(1024.0*1024.0))
}
