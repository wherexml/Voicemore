package main

func init() {
	texts["shortcutUnsafe"] = [3]string{"Use Command, Control, Option or Fn with a typing key.", "一般按鍵須搭配 Command、Control、Option 或 Fn。", "普通按键须搭配 Command、Control、Option 或 Fn。"}
	texts["singleHanContext"] = [3]string{"This single-character correction needs an unambiguous original phrase. Add it as a term hint instead.", "單字替換需要明確的原句範圍，請改為加入詞語提示。", "单字替换需要明确的原句范围，请改为添加词语提示。"}
	texts["settingsUnreadable"] = [3]string{"Settings could not be recovered. Dictation is disabled to protect private apps. The original files have been preserved.\n%s", "無法復原設定，已停用聽寫以保護私密 App。原始檔案已保留。\n%s", "无法恢复设置，已停用听写以保护私密应用。原始文件已保留。\n%s"}
	texts["historySaveFailed"] = [3]string{"History could not be saved: %s", "無法儲存歷史記錄：%s", "无法保存历史记录：%s"}
	texts["eCleanupTruncated"] = [3]string{"The model reached its output limit. The original text is retained; retry or use the original.", "模型已達輸出上限。原文已保留，請重試或使用原文。", "模型已达输出上限。原文已保留，请重试或使用原文。"}
	texts["eCleanupTooLarge"] = [3]string{"The response exceeds 4 MiB. The original text is retained; retry or use the original.", "回應超過 4 MiB。原文已保留，請重試或使用原文。", "响应超过 4 MiB。原文已保留，请重试或使用原文。"}
}
