package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

const (
	apiURL  = "https://api.elevenlabs.io/v1/text-to-speech/"
	voiceID = "XB0fDUnXU5powFXDhCwa" // Charlotte - 한국어 지원 자연스러운 여성 목소리
	outDir  = "/home/ubuntu/onedrive/tts"
)

var (
	flagVoice  string
	flagOutput string
	flagList   bool
)

var speakCmd = &cobra.Command{
	Use:   "speak [텍스트]",
	Short: "텍스트를 음성으로 변환하여 OneDrive/tts 에 저장",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runSpeak,
}

func init() {
	rootCmd.AddCommand(speakCmd)
	speakCmd.Flags().StringVarP(&flagVoice, "voice", "v", voiceID, "ElevenLabs Voice ID")
	speakCmd.Flags().StringVarP(&flagOutput, "output", "o", "", "저장 파일명 (기본: 타임스탬프)")
}

func runSpeak(cmd *cobra.Command, args []string) error {
	apiKey := os.Getenv("ELEVENLABS_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("ELEVENLABS_API_KEY 환경변수가 설정되지 않았습니다")
	}

	text := args[0]
	if len(args) > 1 {
		for _, a := range args[1:] {
			text += " " + a
		}
	}

	fmt.Printf("🎙️  변환 중: %q\n", truncate(text, 50))

	audio, err := callElevenLabs(apiKey, flagVoice, text)
	if err != nil {
		return fmt.Errorf("API 오류: %w", err)
	}

	outPath, err := saveAudio(audio, flagOutput)
	if err != nil {
		return fmt.Errorf("저장 오류: %w", err)
	}

	fmt.Printf("✅ 저장 완료: %s\n", outPath)
	return nil
}

type ttsRequest struct {
	Text          string      `json:"text"`
	ModelID       string      `json:"model_id"`
	VoiceSettings voiceConfig `json:"voice_settings"`
}

type voiceConfig struct {
	Stability       float64 `json:"stability"`
	SimilarityBoost float64 `json:"similarity_boost"`
}

func callElevenLabs(apiKey, voice, text string) ([]byte, error) {
	body := ttsRequest{
		Text:    text,
		ModelID: "eleven_multilingual_v2",
		VoiceSettings: voiceConfig{
			Stability:       0.5,
			SimilarityBoost: 0.75,
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

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

func saveAudio(data []byte, name string) (string, error) {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", err
	}

	if name == "" {
		name = time.Now().Format("20060102_150405") + ".mp3"
	}
	if filepath.Ext(name) == "" {
		name += ".mp3"
	}

	path := filepath.Join(outDir, name)
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
