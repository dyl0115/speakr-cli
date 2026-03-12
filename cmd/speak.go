package cmd

import (
	"bytes"
	"encoding/base64"
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
	apiURL       = "https://api.elevenlabs.io/v1/text-to-speech/"
	googleTTSURL  = "https://texttospeech.googleapis.com/v1/text:synthesize"
	geminiTTSURL  = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent"
	outDir       = "/home/ubuntu/onedrive/tts"
	ntfyTopic    = "dd-claude-x9k4m2"
	edgeTTS      = "/home/ubuntu/.local/bin/edge-tts"
	sendCLI      = "/home/ubuntu/commands/send/send"
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

// Gemini TTS 목소리 프리셋
var geminiVoices = map[string]string{
	"aoede":   "Aoede",
	"leda":    "Leda",
	"puck":    "Puck",
	"charon":  "Charon",
	"kore":    "Kore",
	"female":  "Leda",
	"default": "Leda",
}

// Google Chirp 3: HD 목소리 프리셋
var googleVoices = map[string]string{
	"aoede":      "ko-KR-Chirp3-HD-Aoede",
	"leda":        "ko-KR-Chirp3-HD-Leda",
	"callirrhoe": "ko-KR-Chirp3-HD-Callirrhoe",
	"female":      "ko-KR-Chirp3-HD-Aoede",
	"default":     "ko-KR-Chirp3-HD-Leda",
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
	speakCmd.Flags().StringVarP(&flagVoice, "voice", "v", "leda", "목소리 프리셋 (female, male) 또는 Voice ID 직접 입력")
	speakCmd.Flags().StringVarP(&flagOutput, "output", "o", "", "저장 파일명 (기본: 타임스탬프)")
	speakCmd.Flags().StringVarP(&flagBackend, "backend", "b", "google", "TTS 백엔드 (elevenlabs, edge, google)")
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

func resolveGoogleVoice(v string) string {
	if id, ok := googleVoices[v]; ok {
		return id
	}
	return v
}

func resolveGeminiVoice(v string) string {
	if id, ok := geminiVoices[v]; ok {
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
	case "google":
		outPath, err = runGoogleTTS(text)
	case "gemini":
		outPath, err = runGeminiTTS(text)
	default:
		outPath, err = runElevenLabs(text)
	}

	if err != nil {
		return err
	}

	fmt.Printf("✅ 저장 완료: %s\n", outPath)

	if flagBackend == "edge" || flagBackend == "gemini" {
		sendTelegram(outPath, text)
	} else {
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

// ── Gemini TTS 백엔드 ───────────────────────────────────────────
func runGeminiTTS(text string) (string, error) {
	// gemini는 WAV 출력
	if flagOutput == "" {
		flagOutput = time.Now().Format("20060102_150405") + ".wav"
	} else if filepath.Ext(flagOutput) == ".mp3" {
		flagOutput = strings.TrimSuffix(flagOutput, ".mp3") + ".wav"
	}
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("GEMINI_API_KEY 환경변수가 설정되지 않았습니다")
	}

	voiceName := resolveGeminiVoice(flagVoice)
	model := "gemini-2.5-flash-preview-tts"
	audio, err := callGeminiTTS(apiKey, model, voiceName, text)
	if err != nil {
		return "", fmt.Errorf("Gemini TTS API 오류: %w", err)
	}

	return saveAudio(audio, flagOutput)
}

type geminiTTSRequest struct {
	Contents       []geminiContent      `json:"contents"`
	GenerationConfig geminiGenConfig    `json:"generationConfig"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenConfig struct {
	ResponseModalities []string        `json:"responseModalities"`
	SpeechConfig       geminiSpeechCfg `json:"speechConfig"`
}

type geminiSpeechCfg struct {
	VoiceConfig geminiVoiceCfg `json:"voiceConfig"`
}

type geminiVoiceCfg struct {
	PrebuiltVoiceConfig geminiPrebuiltVoice `json:"prebuiltVoiceConfig"`
}

type geminiPrebuiltVoice struct {
	VoiceName string `json:"voiceName"`
}

type geminiTTSResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				InlineData struct {
					MimeType string `json:"mimeType"`
					Data     string `json:"data"`
				} `json:"inlineData"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func callGeminiTTS(apiKey, model, voiceName, text string) ([]byte, error) {
	body := geminiTTSRequest{
		Contents: []geminiContent{
			{Parts: []geminiPart{{Text: text}}},
		},
		GenerationConfig: geminiGenConfig{
			ResponseModalities: []string{"AUDIO"},
			SpeechConfig: geminiSpeechCfg{
				VoiceConfig: geminiVoiceCfg{
					PrebuiltVoiceConfig: geminiPrebuiltVoice{VoiceName: voiceName},
				},
			},
		},
	}

	jsonBody, _ := json.Marshal(body)
	reqURL := fmt.Sprintf(geminiTTSURL, model) + "?key=" + apiKey

	req, err := http.NewRequest("POST", reqURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

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

	var ttsResp geminiTTSResponse
	if err := json.NewDecoder(resp.Body).Decode(&ttsResp); err != nil {
		return nil, fmt.Errorf("응답 파싱 오류: %w", err)
	}

	if len(ttsResp.Candidates) == 0 || len(ttsResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("빈 오디오 응답")
	}

	audioData := ttsResp.Candidates[0].Content.Parts[0].InlineData.Data
	pcmData, err := base64.StdEncoding.DecodeString(audioData)
	if err != nil {
		return nil, fmt.Errorf("base64 디코딩 오류: %w", err)
	}

	// PCM → WAV 헤더 추가 (24000Hz, 16bit, mono)
	wavData := addWAVHeader(pcmData, 24000, 1, 16)
	return wavData, nil
}

func addWAVHeader(pcm []byte, sampleRate, channels, bitsPerSample int) []byte {
	dataSize := len(pcm)
	headerSize := 44
	totalSize := headerSize + dataSize

	buf := make([]byte, totalSize)
	// RIFF chunk
	copy(buf[0:], []byte("RIFF"))
	putUint32LE(buf[4:], uint32(totalSize-8))
	copy(buf[8:], []byte("WAVE"))
	// fmt chunk
	copy(buf[12:], []byte("fmt "))
	putUint32LE(buf[16:], 16)
	putUint16LE(buf[20:], 1) // PCM
	putUint16LE(buf[22:], uint16(channels))
	putUint32LE(buf[24:], uint32(sampleRate))
	putUint32LE(buf[28:], uint32(sampleRate*channels*bitsPerSample/8))
	putUint16LE(buf[32:], uint16(channels*bitsPerSample/8))
	putUint16LE(buf[34:], uint16(bitsPerSample))
	// data chunk
	copy(buf[36:], []byte("data"))
	putUint32LE(buf[40:], uint32(dataSize))
	copy(buf[44:], pcm)
	return buf
}

func putUint32LE(b []byte, v uint32) {
	b[0] = byte(v); b[1] = byte(v >> 8); b[2] = byte(v >> 16); b[3] = byte(v >> 24)
}

func putUint16LE(b []byte, v uint16) {
	b[0] = byte(v); b[1] = byte(v >> 8)
}

// ── Google Cloud TTS 백엔드 ──────────────────────────────────────
func runGoogleTTS(text string) (string, error) {
	apiKey := os.Getenv("GOOGLE_TTS_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("GOOGLE_TTS_API_KEY 환경변수가 설정되지 않았습니다")
	}

	voiceName := resolveGoogleVoice(flagVoice)
	audio, err := callGoogleTTS(apiKey, voiceName, text)
	if err != nil {
		return "", fmt.Errorf("Google TTS API 오류: %w", err)
	}

	return saveAudio(audio, flagOutput)
}

type googleTTSRequest struct {
	Input       googleTTSInput  `json:"input"`
	Voice       googleTTSVoice  `json:"voice"`
	AudioConfig googleTTSAudio  `json:"audioConfig"`
}

type googleTTSInput struct {
	Text string `json:"text"`
}

type googleTTSVoice struct {
	LanguageCode string `json:"languageCode"`
	Name         string `json:"name"`
}

type googleTTSAudio struct {
	AudioEncoding string `json:"audioEncoding"`
}

type googleTTSResponse struct {
	AudioContent string `json:"audioContent"`
}

func callGoogleTTS(apiKey, voiceName, text string) ([]byte, error) {
	body := googleTTSRequest{
		Input: googleTTSInput{Text: text},
		Voice: googleTTSVoice{
			LanguageCode: "ko-KR",
			Name:         voiceName,
		},
		AudioConfig: googleTTSAudio{AudioEncoding: "MP3"},
	}

	jsonBody, _ := json.Marshal(body)
	reqURL := googleTTSURL + "?key=" + apiKey

	req, err := http.NewRequest("POST", reqURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

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

	var ttsResp googleTTSResponse
	if err := json.NewDecoder(resp.Body).Decode(&ttsResp); err != nil {
		return nil, fmt.Errorf("응답 파싱 오류: %w", err)
	}

	if ttsResp.AudioContent == "" {
		return nil, fmt.Errorf("빈 오디오 응답")
	}

	return base64.StdEncoding.DecodeString(ttsResp.AudioContent)
}

// ── 공통 유틸 ────────────────────────────────────────────────────
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
