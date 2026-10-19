package main

import (
	"strings"
	"voiceinput/internal/session"
)

func init() {
	texts["errorNoAudio"] = [3]string{"No sound recorded. Check your microphone.", "未錄到聲音，請檢查麥克風", "未录到声音，请检查麦克风"}
	texts["errorNoText"] = [3]string{"No text recognized. Try again.", "未識別到文字，請重試", "未识别到文字，请重试"}
	texts["errorDictation"] = [3]string{"Dictation failed. Try again.", "聽寫失敗，請重試", "听写失败，请重试"}
	texts["errorCleanup"] = [3]string{"Text cleanup failed. Try again.", "文字整理失敗，請重試", "文字整理失败，请重试"}
	texts["errorMicrophone"] = [3]string{"Check microphone access and settings.", "請檢查麥克風權限與設定", "请检查麦克风权限与设置"}
	texts["errorHistory"] = [3]string{"History could not be saved.", "歷史記錄儲存失敗", "历史记录保存失败"}
}

// Only the presentation copy is shortened. Controller state and history retain
// the original diagnostic message and task ID. Unknown causes stay generic.
func conciseFailure(v session.View) session.View {
	if v.Phase != session.Failed {
		return v
	}
	key := "errorDictation"
	switch {
	case strings.HasPrefix(v.Message, "sub2api ASR_EMPTY_RESULT (task "):
		key = "errorNoText"
		if v.SilentAudio {
			key = "errorNoAudio"
		}
	case v.CleanupFailed:
		key = "errorCleanup"
	case v.Message == T("micNoFrames"):
		key = "errorNoAudio"
	case v.Message == T("micMissing"), v.Message == T("micDenied"), v.Message == T("micNotGranted"):
		key = "errorMicrophone"
	case v.Retained && strings.HasPrefix(v.Message, strings.Split(T("historySaveFailed", "\x00"), "\x00")[0]):
		key = "errorHistory"
	}
	v.Message = T(key)
	if v.Notice != "" {
		v.Notice = v.Message
	}
	return v
}
