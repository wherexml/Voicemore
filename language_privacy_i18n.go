package main

func init() {
	texts["asrLanguage"] = [3]string{"Recognition language", "識別語言", "识别语言"}
	texts["asrLang_zh"] = [3]string{"Chinese", "中文", "中文"}
	texts["asrLang_auto"] = [3]string{"Auto-detect", "自動偵測", "自动识别"}
	texts["asrLang_en"] = [3]string{"English", "英文", "英语"}
	texts["asrLanguageOld"] = [3]string{"The on-device speech model needs a restart for this language.", "本機語音模型需重新啟動才能使用此語言。", "本地语音模型需重新启动才能使用此语言。"}
	texts["noteLanguage"] = [3]string{"speech language: %s", "語音語言：%s", "语音语言：%s"}
	texts["privateApps"] = [3]string{"Excluded apps", "不留歷史的應用", "不留历史的应用"}
	texts["privateAppsHint"] = [3]string{"Inserts as usual; no history, audio or learning", "照常寫入；不留歷史、錄音或學習", "照常写入；不留历史、录音或学习"}
}
