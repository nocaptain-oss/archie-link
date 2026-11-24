package cli

import (
	"strings"
	"testing"

	qrcode "github.com/skip2/go-qrcode"
)

// TestRenderTerminalQRRoundTrip 从渲染输出的半块字符反解位图，
// 与 qrcode.Bitmap() 逐位对比，验证字符映射与 ANSI 包装无失真。
func TestRenderTerminalQRRoundTrip(t *testing.T) {
	for _, content := range []string{
		"https://nocaptain-oss.github.io/archie-link/",
		"alipays://platformapi/startapp?appId=2021006180624128&page=pages/order/index?purchaseLinkId=Tk9XUEFZOlBVUkNIQVNFOmQ1OTBkMGQ2ZjVlMjQzMDFiMzk5NGU3NTQxMGE4ZWM0XyQwNw",
		"https://render.alipay.com/p/s/i/?mx=DIRECT_91e4ba51675334238c6a296b13293cd4",
	} {
		rendered, err := renderTerminalQR(content)
		if err != nil {
			t.Fatalf("render %q: %v", content[:32], err)
		}
		code, err := qrcode.New(content, qrcode.Medium)
		if err != nil {
			t.Fatal(err)
		}
		want := code.Bitmap()
		lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
		if len(lines)*2 < len(want)-1 {
			t.Fatalf("rendered %d lines for %d bitmap rows", len(lines), len(want))
		}
		for y := 0; y < len(want); y += 2 {
			line := strings.TrimSuffix(strings.TrimPrefix(lines[y/2], "\033[30;47m"), "\033[0m")
			runes := []rune(line)
			if len(runes) != len(want[y]) {
				t.Fatalf("line %d width %d, want %d", y/2, len(runes), len(want[y]))
			}
			for x := 0; x < len(want[y]); x++ {
				top := want[y][x]
				bottom := y+1 < len(want) && want[y+1][x]
				var wantRune rune
				switch {
				case top && bottom:
					wantRune = '█'
				case top:
					wantRune = '▀'
				case bottom:
					wantRune = '▄'
				default:
					wantRune = ' '
				}
				if runes[x] != wantRune {
					t.Fatalf("content %q: mismatch at (%d,%d): got %q want %q",
						content[:32], x, y, runes[x], wantRune)
				}
			}
		}
	}
}

func TestRenderTerminalQRRejectsEmpty(t *testing.T) {
	if _, err := renderTerminalQR("  "); err == nil {
		t.Fatal("empty content must be rejected")
	}
}
