# speakr-cli

ElevenLabs TTS CLI - 텍스트를 음성으로 변환하여 OneDrive에 저장하는 Go CLI 도구

## 설치

```bash
git clone https://github.com/dyl0115/speakr-cli.git
cd speakr-cli
go build -o speakr .
cp speakr ~/go/bin/
```

## 환경변수 설정

```bash
export ELEVENLABS_API_KEY=your_api_key_here
export SPEAKR_OUT_DIR=/path/to/output  # 기본값: ~/onedrive/tts
```

## 사용법

```bash
# 기본 사용
speakr speak "안녕하세요"

# 파일명 지정
speakr speak "오늘 할 일" -o today

# 보이스 변경
speakr speak "Hello" -v VOICE_ID
```

## 저장 위치

기본값: `~/onedrive/tts/` (rclone OneDrive 마운트 경로)

파일명은 `YYYYMMDD_HHMMSS.mp3` 형식으로 자동 생성됩니다.

## 기본 보이스

Charlotte (eleven_multilingual_v2) - 한국어/영어 지원