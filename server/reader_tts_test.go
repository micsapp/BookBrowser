package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestEPUBReaderTTSModesAndBackgroundControls(t *testing.T) {
	s := newAuthTestServer(t)

	read := func(path string) string {
		t.Helper()
		response := requestServer(s, http.MethodGet, path, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body.String())
		}
		return response.Body.String()
	}

	index := read("/static/reader/epub/")
	for _, expected := range []string{
		`id="tts-options-panel"`,
		`name="tts-mode" value="continuous"`,
		`name="tts-mode" value="timed"`,
		`id="tts-duration-minutes"`,
		`id="tts-keep-screen-on"`,
		`id="tts-audio"`,
		`playsinline`,
	} {
		if !strings.Contains(index, expected) {
			t.Errorf("EPUB TTS controls are missing %q", expected)
		}
	}
	if count := strings.Count(index, `id="tts-audio"`); count != 1 {
		t.Errorf("persistent TTS audio element count=%d, want 1", count)
	}

	script := read("/static/reader/epub/script.js")
	for _, expected := range []string{
		`App.prototype.startTTSTimer`,
		`App.prototype.pauseTTSTimer`,
		`App.prototype.setupTTSMediaSession`,
		`navigator.mediaSession.setActionHandler`,
		`App.prototype.requestTTSWakeLock`,
		`navigator.wakeLock.request("screen")`,
		`App.prototype.onTTSVisibilityChange`,
		`App.prototype.fetchTTSBlob`,
		`that.qs("#tts-audio")`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("EPUB TTS implementation is missing %q", expected)
		}
	}
	if strings.Contains(script, "new Audio(") {
		t.Error("EPUB TTS must reuse its persistent audio element")
	}

	style := read("/static/reader/epub/style.css")
	for _, expected := range []string{".tts-options-panel", ".tts-mode-option", ".tts-wake-option"} {
		if !strings.Contains(style, expected) {
			t.Errorf("EPUB TTS styles are missing %q", expected)
		}
	}

	worker := read("/sw.js")
	if !strings.Contains(worker, `CACHE_NAME = CACHE_PREFIX + "v4"`) {
		t.Error("PWA cache version was not advanced for the new reader assets")
	}
}
