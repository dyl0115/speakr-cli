package cmd

import ("github.com/spf13/cobra")

var rootCmd = &cobra.Command{Use:"speakr",Short:"ElevenLabs TTS CLI"}

func Execute() { rootCmd.Execute() }
