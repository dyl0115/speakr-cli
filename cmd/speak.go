package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	apiURL    = "https://api.elevenlabs.io/v1/text-to-speech/"
	outDir    = "/home/ubuntu/onedrive/tts"
	ntfyTopic = "dd-claude-x9k4m2"
	edgeTTS   = "/home/ubuntu/.local/bin/edge-tts"
	sendCLI   = "/home/ubuntu/commands/send/send"
)

// ElevenLabs 목소리 프리셋
var voices = map[string]string{
	"female":  "uyVNoMrnUku1dZyVEXwD",
	"male":    "ZJCNdZEjYwkOElxugmW2",
	"default": "uyVNoMrnUku1dZyVEXwD",
}

// edge-tts 목소리 프리셋
var edgeVoices = map[string]string{
	"female":  "ko-KR-SunHiNeural",
	"male":    "ko-KR-InJoonNeural",
	"default": "ko-KR-SunHiNeural",
}

var (
	flagVoice   string
	flagOutput  string
	flagBackend string
)

var speakCmd = &cobra.Command{
	Use:   "speak [텍스트]",
	Short: "텍스트를 음성으로 변환하여 OneDrive/tts 에 저장",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runSpeak,
}

func init() {
	rootCmd.AddCommand(speakCmd)
	speakCmd.Flags().StringVarP(&flagVoice, "voice", "v", "female", "목소리 프리셋 (female, male) 또는 Voice ID 직접 입력")
	speakCmd.Flags().StringVarP(&flagOutput, "output", "o", "", "저장 파일명 (기본: 타임스탬프)")
	speakCmd.Flags().StringVarP(&flagBackend, "backend", "b", "elevenlabs", "TTS 백엔드 (elevenlabs, edge)")
}

func resolveVoice(v string) string {
	if id, ok := voices[v]; ok {
		return id
	}
	return v
}

func resolveEdgeVoice(v string) string {
	if id, ok := edgeVoices[v]; ok {
		return id
	}
	return v
}

func runSpeak(cmd *cobra.Command, args []string) error {
	text := strings.Join(args, " ")

	fmt.Printf("🎙️  변환 중: %q [%s / %s]\n", truncate(text, 50), flagBackend, flagVoice)

	var outPath string
	var err error

	switch flagBackend {
	case "edge":
		outPath, err = runEdgeTTS(text)
	default:
		outPath, err = runElevenLabs(text)
	}

	if err != nil {
		return err
	}

	fmt.Printf("✅ 저장 완료: %s\n", outPath)

	if flagBackend == "edge" {
		// edge 백엔드: 텔레그램으로 전송
		sendTelegram(outPath, text)
	} else {
		// elevenlabs 백엔드: ntfy 알림
		sendNtfy(filepath.Base(outPath), truncate(text, 40))
	}

	return nil
}

// ── edge-tts 백엔드 ──────────────────────────────────────────────
func runEdgeTTS(text string) (string, error) {
	outPath, err := buildOutPath(flagOutput)
	if err != nil {
		return "", fmt.Errorf("경로 오류: %w", err)
	}

	voice := resolveEdgeVoice(flagVoice)
	c := exec.Command(edgeTTS, "--text", text, "--voice", voice, "--write-media", outPath)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	if err := c.Run(); err != nil {
		return "", fmt.Errorf("edge-tts 오류: %w", err)
	}
	return outPath, nil
}

func sendTelegram(path, text string) {
	sendBin := sendCLI
	// send 바이너리 경로 fallback
	if _, err := os.Stat(sendBin); err != nil {
		if p, err := exec.LookPath("send"); err == nil {
			sendBin = p
		} else {
			fmt.Printf("⚠️  send CLI를 찾을 수 없습니다\n")
			return
		}
	}

	preview := truncate(text, 40)
	c := exec.Command(sendBin, "telegram", "-a", path, "-t", "🎙️ "+preview)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	if err := c.Run(); err != nil {
		fmt.Printf("⚠️  텔레그램 전송 실패: %v\n", err)
		return
	}
	fmt.Printf("📨 텔레그램 전송 완료!\n")
}

// ── ElevenLabs 백엔드 ────────────────────────────────────────────
func runElevenLabs(text string) (string, error) {
	apiKey := os.Getenv("ELEVENLABS_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("ELEVENLABS_API_KEY 환경변수가 설정되지 않았습니다")
	}

	voiceID := resolveVoice(flagVoice)
	audio, err := callElevenLabs(apiKey, voiceID, text)
	if err != nil {
		return "", fmt.Errorf("API 오류: %w", err)
	}

	return saveAudio(audio, flagOutput)
}

func sendNtfy(fileName, preview string) {
	url := "https://ntfy.sh/" + ntfyTopic
	body := strings.NewReader(preview)

	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return
	}
	req.Header.Set("Title", "🎙️ 클로드가 말했어!")
	req.Header.Set("Tags", "speech_balloon")
	req.Header.Set("Priority", "default")
	req.Header.Set("X-Filename", fileName)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("⚠️  ntfy 알림 실패: %v\n", err)
		return
	}
	defer resp.Body.Close()
	fmt.Printf("🔔 ntfy 알림 전송 완료!\n")
}

type ttsRequest struct {
	Text          string      `json:"text"`
	ModelID       string      `json:"model_id"`
	LanguageCode  string      `json:"language_code"`
	VoiceSettings voiceConfig `json:"voice_settings"`
}

type voiceConfig struct {
	Stability       float64 `json:"stability"`
	SimilarityBoost float64 `json:"similarity_boost"`
	Style           float64 `json:"style"`
	UseSpeakerBoost bool    `json:"use_speaker_boost"`
}

func callElevenLabs(apiKey, voice, text string) ([]byte, error) {
	body := ttsRequest{
		Text:         text,
		ModelID:      "eleven_multilingual_v2",
		LanguageCode: "ko",
		VoiceSettings: voiceConfig{
			Stability:       0.5,
			SimilarityBoost: 0.75,
			Style:           0.3,
			UseSpeakerBoost: true,
		},
	}

	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", apiURL+voice, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("xi-api-key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/mpeg")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(errBody))
	}

	return io.ReadAll(resp.Body)
}

func buildOutPath(name string) (string, error) {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", err
	}
	if name == "" {
		name = time.Now().Format("20060102_150405") + ".mp3"
	}
	if filepath.Ext(name) == "" {
		name += ".mp3"
	}
	return filepath.Join(outDir, name), nil
}

func saveAudio(data []byte, name string) (string, error) {
	path, err := buildOutPath(name)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	return path, nil
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
