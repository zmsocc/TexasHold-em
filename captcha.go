package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/patrickmn/go-cache"
)

var captchaCache = cache.New(5*time.Minute, 10*time.Minute)

// CaptchaData 验证码数据
type CaptchaData struct {
	Code        string
	ImageBase64 string
}

// GenerateCaptcha 生成验证码
func GenerateCaptcha(clientIP string) (*CaptchaData, error) {
	// 生成4位随机数字验证码
	code := generateRandomCode(4)

	// 生成验证码图片
	img := createCaptchaImage(code)

	// 转换为 base64
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	imageBase64 := base64.StdEncoding.EncodeToString(buf.Bytes())

	// 存储验证码
	captchaCache.Set(clientIP, code, 5*time.Minute)

	return &CaptchaData{
		Code:        code,
		ImageBase64: imageBase64,
	}, nil
}

// VerifyCaptcha 验证验证码
func VerifyCaptcha(clientIP, code string) bool {
	if storedCode, found := captchaCache.Get(clientIP); found {
		if strings.EqualFold(storedCode.(string), code) {
			captchaCache.Delete(clientIP)
			return true
		}
	}
	return false
}

// generateRandomCode 生成随机数字验证码
func generateRandomCode(length int) string {
	var result strings.Builder
	for i := 0; i < length; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(10))
		result.WriteString(fmt.Sprintf("%d", n.Int64()))
	}
	return result.String()
}

// createCaptchaImage 创建验证码图片
func createCaptchaImage(code string) image.Image {
	width, height := 120, 40
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// 背景色 - 浅灰色
	bgColor := color.RGBA{240, 240, 240, 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{bgColor}, image.Point{}, draw.Src)

	// 添加干扰线
	for i := 0; i < 3; i++ {
		x1, _ := rand.Int(rand.Reader, big.NewInt(int64(width)))
		y1, _ := rand.Int(rand.Reader, big.NewInt(int64(height)))
		x2, _ := rand.Int(rand.Reader, big.NewInt(int64(width)))
		y2, _ := rand.Int(rand.Reader, big.NewInt(int64(height)))
		drawLine(img, int(x1.Int64()), int(y1.Int64()), int(x2.Int64()), int(y2.Int64()))
	}

	// 添加干扰点
	for i := 0; i < 30; i++ {
		x, _ := rand.Int(rand.Reader, big.NewInt(int64(width)))
		y, _ := rand.Int(rand.Reader, big.NewInt(int64(height)))
		img.Set(int(x.Int64()), int(y.Int64()), color.RGBA{100, 100, 100, 255})
	}

	// 绘制数字
	charWidth := width / len(code)
	for i, ch := range code {
		x := i*charWidth + 8
		y := 8
		drawDigit(img, ch, x, y)
	}

	return img
}

// drawDigit 绘制数字（使用点阵方式）
func drawDigit(img *image.RGBA, ch rune, x, y int) {
	// 数字点阵定义 (5x7)
	digits := map[rune][]string{
		'0': {
			" ### ",
			"#   #",
			"#  ##",
			"# # #",
			"##  #",
			"#   #",
			" ### ",
		},
		'1': {
			"  #  ",
			" ##  ",
			"  #  ",
			"  #  ",
			"  #  ",
			"  #  ",
			" ### ",
		},
		'2': {
			" ### ",
			"#   #",
			"    #",
			"   # ",
			"  #  ",
			" #   ",
			"#####",
		},
		'3': {
			" ### ",
			"#   #",
			"    #",
			"  ## ",
			"    #",
			"#   #",
			" ### ",
		},
		'4': {
			"   # ",
			"  ## ",
			" # # ",
			"#  # ",
			"#####",
			"   # ",
			"   # ",
		},
		'5': {
			"#####",
			"#    ",
			"#### ",
			"    #",
			"    #",
			"#   #",
			" ### ",
		},
		'6': {
			" ### ",
			"#   #",
			"#    ",
			"#### ",
			"#   #",
			"#   #",
			" ### ",
		},
		'7': {
			"#####",
			"    #",
			"   # ",
			"  #  ",
			" #   ",
			" #   ",
			" #   ",
		},
		'8': {
			" ### ",
			"#   #",
			"#   #",
			" ### ",
			"#   #",
			"#   #",
			" ### ",
		},
		'9': {
			" ### ",
			"#   #",
			"#   #",
			" ####",
			"    #",
			"#   #",
			" ### ",
		},
	}

	// 数字颜色
	colors := []color.RGBA{
		{0, 100, 200, 255},   // 蓝色
		{200, 50, 50, 255},   // 红色
		{50, 150, 50, 255},   // 绿色
		{200, 100, 0, 255},   // 橙色
	}

	pattern, ok := digits[ch]
	if !ok {
		return
	}

	n := int(ch - '0')
	c := colors[n%len(colors)]

	// 绘制点阵
	for row, line := range pattern {
		for col, pixel := range line {
			if pixel == '#' {
				// 放大绘制 (每个点变成4个像素)
				for dy := 0; dy < 4; dy++ {
					for dx := 0; dx < 4; dx++ {
						img.Set(x+col*4+dx, y+row*4+dy, c)
					}
				}
			}
		}
	}
}

// drawLine 绘制线条
func drawLine(img *image.RGBA, x1, y1, x2, y2 int) {
	dx := abs(x2 - x1)
	dy := abs(y2 - y1)
	sx := 1
	if x1 > x2 {
		sx = -1
	}
	sy := 1
	if y1 > y2 {
		sy = -1
	}
	err := dx - dy

	lineColor := color.RGBA{180, 180, 180, 255}

	for {
		img.Set(x1, y1, lineColor)
		if x1 == x2 && y1 == y2 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x1 += sx
		}
		if e2 < dx {
			err += dx
			y1 += sy
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// HandleCaptcha 处理验证码请求
func HandleCaptcha(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientIP := GetClientIP(r)
	captcha, err := GenerateCaptcha(clientIP)
	if err != nil {
		http.Error(w, "Failed to generate captcha", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"image":   "data:image/png;base64," + captcha.ImageBase64,
	})
}
