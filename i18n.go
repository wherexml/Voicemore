package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"voiceinput/internal/asr"
	"voiceinput/internal/cleanup"
	"voiceinput/internal/cloudasr"
	"voiceinput/internal/config"
	"voiceinput/internal/dict"
	"voiceinput/internal/providers"
)

// The interface is in English, Traditional Chinese or Simplified Chinese.
// The choice is saved in the settings; without one it follows the system.

var (
	langIDs   = []string{"en", "zh-Hant", "zh-Hans"}
	langNames = []string{"English", "繁體中文", "简体中文"}
	langIndex atomic.Int32
)

func init() {
	texts["modeToggleShort"] = [3]string{"Toggle", "切換", "切换"}
	texts["modeHoldShort"] = [3]string{"Hold", "按住", "按住"}
	texts["catDictation"] = [3]string{"Dictation", "聽寫", "听写"}
	texts["catOutput"] = [3]string{"Text & paste", "文字與貼上", "文字与粘贴"}
	texts["allRecords"] = [3]string{"All records", "全部記錄", "全部记录"}
	texts["today"] = [3]string{"Today", "今天", "今天"}
	texts["homeTry"] = [3]string{"Try dictating", "在這裡試一次", "在这里试一次"}
	texts["homeCountdown"] = [3]string{"Starting in 3 seconds — switch to your input field", "3 秒後開始，請切換到輸入位置", "3 秒后开始，请切换到输入位置"}
	texts["homeFinish"] = [3]string{"Finish & insert", "結束並寫入", "结束并写入"}

	texts["cleanupGroup"] = [3]string{"Text cleanup", "文字整理", "文字整理"}
	texts["polishRules"] = [3]string{"Polishing rules", "潤色規則", "润色规则"}
	texts["polishExpand"] = [3]string{"Expand", "展開", "展开"}
	texts["polishCollapse"] = [3]string{"Collapse", "收起", "收起"}
	texts["managedCleanupRules"] = [3]string{"This cloud service manages its own formatting and rules", "此雲端服務自行管理格式與潤色規則", "此云服务自行管理格式与润色规则"}
	texts["basicFormat"] = [3]string{"Basic formatting", "基礎格式", "基础格式"}
	texts["formatPeriod"] = [3]string{"Omit the final period", "省略末尾句號", "省略末尾句号"}
	texts["formatPeriodHint"] = [3]string{"Keep question marks and ellipses.", "保留問號與省略號。", "保留问号与省略号。"}
	texts["formatCalm"] = [3]string{"Reduce exclamation marks", "減少驚嘆號", "减少感叹号"}
	texts["formatCalmHint"] = [3]string{"Use periods for ordinary prose; preserve quotes and code.", "一般敘述改用句號，保留引文與程式碼。", "普通叙述改用句号，保留引文与代码。"}
	texts["formatFillers"] = [3]string{"Remove common fillers", "移除常見語氣詞", "删除常见语气词"}
	texts["formatFillersHint"] = [3]string{"Remove standalone 嗯 / 呃 / 啊 and sentence-ending 啊. No cleanup model needed.", "移除獨立的嗯／呃／啊和句尾啊，無需整理模型。", "删除独立的嗯／呃／啊和句尾啊，无需整理模型。"}
	texts["formatNumbers"] = [3]string{"Use Arabic numerals", "數字使用阿拉伯數字", "数字使用阿拉伯数字"}
	texts["formatNumbersHint"] = [3]string{"Write clear numbers as digits, e.g. 二零二六年 → 2026年. Keep quoted text unchanged.", "將明確數字轉為 2026年、15個等形式，保留引用原文。", "将明确数字转为 2026年、15个等形式，保留引用原文。"}
	texts["formatAcronyms"] = [3]string{"Join spelled-out terms", "合併拼讀詞彙", "合并拼读词汇"}
	texts["formatAcronymsHint"] = [3]string{"Join known terms such as A P P → APP and dictionary words.", "合併 A P P → APP 等已知縮寫及詞典詞彙。", "合并 A P P → APP 等已知缩写及词典词汇。"}

	texts["gAudio"] = [3]string{"Audio", "音訊", "音频"}
	texts["sMicDevice"] = [3]string{"Microphone", "麥克風", "麦克风"}
	texts["sMicDeviceHint"] = [3]string{"The input used for dictation.", "聽寫使用的輸入裝置。", "听写使用的输入设备。"}
	texts["micAuto"] = [3]string{"Automatic (system default)", "自動（系統預設）", "自动（系统默认）"}
	texts["micAutoNamed"] = [3]string{"Automatic (%s)", "自動（%s）", "自动（%s）"}
	texts["micDisconnected"] = [3]string{"%s (disconnected)", "%s（未連接）", "%s（未连接）"}
	texts["micDisconnectedHint"] = [3]string{"Not connected: dictation uses the system default until it is back.", "未連接：重新連接前，聽寫會使用系統預設麥克風。", "未连接：重新连接前，听写会使用系统默认麦克风。"}
	texts["sSounds"] = [3]string{"Interaction sounds", "互動音效", "交互音效"}
	texts["sSoundsHint"] = [3]string{"Play a short sound when recording starts and stops.", "開始與結束錄音時播放提示音。", "开始和结束录音时播放提示音。"}
	texts["sMuteOutput"] = [3]string{"Mute audio while dictating", "聽寫時靜音其他音訊", "听写时静音其他音频"}
	texts["sMuteOutputHint"] = [3]string{"Mutes the sound output while recording and restores it when recording ends.", "錄音時將聲音輸出靜音，錄音結束後恢復。", "录音时将声音输出静音，录音结束后恢复。"}
	texts["sLoginItem"] = [3]string{"Launch at login", "登入時啟動", "登录时启动"}
	texts["sLoginItemHint"] = [3]string{"Open Voicemore automatically when you log in to your Mac.", "登入 Mac 時自動開啟 Voicemore。", "登录 Mac 时自动打开 Voicemore。"}
	texts["loginApproval"] = [3]string{"Waiting for approval in System Settings → Login Items.", "需在「系統設定」→「登入項目」中允許。", "需在“系统设置”→“登录项”中允许。"}
	texts["loginUnavailable"] = [3]string{"Only available for the installed app.", "僅適用於已安裝的 App。", "仅适用于已安装的 App。"}
	texts["loginFailed"] = [3]string{"Could not change the login item: %s", "無法變更登入項目：%s", "无法更改登录项：%s"}
	texts["sDock"] = [3]string{"Show in Dock", "在 Dock 中顯示", "在程序坞中显示"}
	texts["sDockHint"] = [3]string{"When off, Voicemore stays available from the menu bar.", "關閉後，仍可從選單列使用 Voicemore。", "关闭后，仍可从菜单栏使用 Voicemore。"}

	texts["cleanupNotConfigured"] = [3]string{"Text cleanup model is not configured", "未配置文字整理模型", "未配置文字整理模型"}
	texts["cleanupChecking"] = [3]string{"Checking service…", "正在檢查服務…", "正在检查服务…"}
	texts["cleanupUnavailable"] = [3]string{
		"Text cleanup is not configured or its service/model is unavailable. It remains off. Configure and check a text model in Settings → Models. The bundled speech model does not include a text cleanup service.",
		"整理文字尚未配置，或服務／模型不可用，已保持關閉。請到設定 → 模型配置並檢查文字模型。內建語音模型不包含文字整理服務。",
		"整理文字尚未配置，或服务／模型不可用，已保持关闭。请到设置 → 模型配置并检查文字模型。内置语音模型不包含文字整理服务。",
	}
}

// langFor maps a saved choice, or a system locale such as "zh-TW", to a
// language index.
func langFor(id string) int {
	low := strings.ToLower(id)
	switch {
	case strings.HasPrefix(low, "zh"):
		if strings.Contains(low, "hant") || strings.Contains(low, "tw") || strings.Contains(low, "hk") || strings.Contains(low, "mo") {
			return 1
		}
		return 2
	}
	return 0
}

func setLang(i int) { langIndex.Store(int32(i)) }

// T returns a text in the current language, formatted with args when it
// has any. A key without a translation falls back to English, and an
// unknown key shows as itself, which is easy to spot.
func T(key string, args ...any) string {
	row, ok := texts[key]
	if !ok {
		return key
	}
	s := row[langIndex.Load()]
	if s == "" {
		s = row[0]
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// explain puts an error of the model services in words for the user. The
// innermost cause is kept as the system reports it.
func explain(err error) string {
	var status *cleanup.StatusError
	var service *cloudasr.ServiceError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, asr.ErrNoAudioSignal):
		return T("errorNoAudio")
	case errors.As(err, &service):
		return T("eCloudService", service.Code, service.Message)
	case errors.Is(err, cloudasr.ErrAuth):
		return T("eCloudAuth")
	case errors.Is(err, cloudasr.ErrUnreachable):
		return T("eCloudUnreachable", rootCause(err))
	case errors.Is(err, cloudasr.ErrProtocol):
		return T("eCloudProtocol", rootCause(err))
	case errors.Is(err, providers.ErrNotSet):
		return T("eNotSet")
	case errors.Is(err, providers.ErrUnconfigured):
		return T("eUnconfigured")
	case errors.Is(err, providers.ErrNoAdapter):
		return T("eNoAdapter")
	case errors.Is(err, cleanup.ErrThinkingParams):
		return T("errThinkingParams")
	case errors.Is(err, cleanup.ErrNotConfigured):
		return T("eNotConfigured")
	case errors.Is(err, cleanup.ErrBadURL), errors.Is(err, asr.ErrBadURL):
		return T("eBadURL")
	case errors.Is(err, context.DeadlineExceeded):
		return T("eTimeout")
	case errors.Is(err, context.Canceled):
		return T("eCancelled")
	case errors.Is(err, cleanup.ErrUnreachable), errors.Is(err, asr.ErrUnreachable):
		return T("eUnreachable", rootCause(err))
	case errors.As(err, &status):
		switch status.Code {
		case 401:
			return T("eCleanupAuth", status.Model)
		case 403:
			return T("eCleanupForbidden", status.Model)
		case 404:
			return T("eNotFound")
		}
		return T("eStatus", status.Status)
	case errors.Is(err, cleanup.ErrNoText):
		return T("eNoText")
	case errors.Is(err, cleanup.ErrTruncated):
		return T("eCleanupTruncated")
	case errors.Is(err, cleanup.ErrResponseTooLarge):
		return T("eCleanupTooLarge")
	case errors.Is(err, config.ErrUnsafeShortcut):
		return T("shortcutUnsafe")
	case errors.Is(err, dict.ErrRuleContext):
		return T("singleHanContext")
	case errors.Is(err, asr.ErrNotASR):
		return T("eNotASR")
	}
	return err.Error()
}

func rootCause(err error) string {
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return err.Error()
		}
		err = next
	}
}

// texts holds every text of the app: English, Traditional Chinese,
// Simplified Chinese. The keys of internal/session's messages are here
// under the same names.
var texts = map[string][3]string{
	// Appearance, in Settings → General.
	"appearance": {"Appearance", "外觀", "外观"},
	"apLight":    {"Light", "淺色", "浅色"},
	"apDark":     {"Dark", "深色", "深色"},
	"apAuto":     {"Automatic", "自動", "自动"},

	// Volcengine, for settings saved in an access mode this version no longer offers.
	"volcLegacyNote": {"This Mac still uses Volcengine credentials saved in an earlier access mode, and they keep working. Pressing Save key here switches to the current mode.", "這台 Mac 仍在使用先前接入方式儲存的火山引擎憑證，它們會繼續有效。在此儲存 API 金鑰後會改用目前的方式。", "这台 Mac 仍在使用之前接入方式保存的火山引擎凭证，它们会继续有效。在此保存 API 密钥后会改用当前方式。"},

	// The first-run guide (onboarding.go).
	"obTitle":        {"Get started", "開始使用", "开始使用"},
	"obProgress":     {"%d of %d done", "已完成 %d / %d", "已完成 %d / %d"},
	"obS1":           {"How it works", "使用方式", "使用方式"},
	"obS2":           {"Speech model", "語音模型", "语音模型"},
	"obS3":           {"Permissions", "權限", "权限"},
	"obS4":           {"First dictation", "第一次聽寫", "第一次听写"},
	"obS1h":          {"Speak, and it's typed", "隨時隨地，開口即寫", "随时随地，开口即写"},
	"obS2h":          {"Choose where speech is recognized", "選擇語音辨識的位置", "选择语音识别的位置"},
	"obS3h":          {"Allow three things in macOS", "在 macOS 允許三項權限", "在 macOS 允许三项权限"},
	"obS4h":          {"Try it once here", "在這裡試一次", "在这里试一次"},
	"obI1":           {"Press %s", "按 %s", "按 %s"},
	"obI1text":       {"In any app, with the cursor where the text should go.", "在任何 App 中，游標放在要輸入的位置。", "在任何应用中，光标放在要输入的位置。"},
	"obI1holdText":   {"Hold it in any app, with the cursor where the text should go.", "在任何 App 中按住，游標放在要輸入的位置。", "在任何应用中按住，光标放在要输入的位置。"},
	"obI2":           {"Speak", "說話", "说话"},
	"obI2text":       {"A small capsule at the bottom of the screen shows it is listening.", "螢幕底部的小膠囊表示正在聆聽。", "屏幕底部的小胶囊表示正在聆听。"},
	"obI3":           {"Text appears", "文字出現", "文字出现"},
	"obI3text":       {"Press %s again: the text is inserted at your cursor.", "再按一次 %s，文字就會寫入游標位置。", "再按一次 %s，文字就会写入光标位置。"},
	"obI3holdText":   {"Release the key: the text is inserted at your cursor.", "放開按鍵，文字就會寫入游標位置。", "松开按键，文字就会写入光标位置。"},
	"obLocal":        {"On this Mac", "在這台 Mac 上", "在这台 Mac 上"},
	"obLocalText":    {"Private and offline after one download.", "下載一次後即可離線使用，內容不離開本機。", "下载一次后即可离线使用，内容不离开本机。"},
	"obCloud":        {"A cloud provider", "雲端供應商", "云端服务商"},
	"obCloudText":    {"No download; needs your own API key.", "不需下載；需要你自己的 API 金鑰。", "无需下载；需要你自己的 API 密钥。"},
	"obMore":         {"More options…", "更多選項…", "更多选项…"},
	"obDownload":     {"Download model", "下載模型", "下载模型"},
	"obDownloadSize": {"About %.1f GB total", "共約 %.1f GB", "共约 %.1f GB"},
	"obLocalNote":    {"Voicemore manages the model and starts it with the app.", "Voicemore 會管理模型，並隨 App 啟動。", "Voicemore 会管理模型，并随应用启动。"},
	"obCloudOpen":    {"Open speech providers", "打開語音辨識供應商", "打开语音识别服务商"},
	"obWhyMic":       {"To hear what you say while you dictate.", "聽寫時用來聽見你說的話。", "听写时用来听见你说的话。"},
	"obWhyAx":        {"To insert the text at your cursor.", "用來把文字寫入游標位置。", "用来把文字写入光标位置。"},
	"obWhyInput":     {"To notice the trigger key in any app.", "用來在任何 App 中偵測觸發鍵。", "用来在任何应用中检测触发键。"},
	"obDenied":       {"Denied", "已拒絕", "已拒绝"},
	"obDeniedNote":   {"Microphone access was denied. macOS will not ask again: allow Voicemore in System Settings, then come back.", "麥克風權限已被拒絕。macOS 不會再次詢問：請在系統設定中允許 Voicemore 後再回來。", "麦克风权限已被拒绝。macOS 不会再次询问：请在系统设置中允许 Voicemore 后再回来。"},
	"obPermsNote":    {"All three are granted.", "三項權限皆已授權。", "三项权限均已授权。"},
	"obField":        {"Practice field", "練習輸入框", "练习输入框"},
	"obFieldPh":      {"Your practice dictation will appear here.", "練習聽寫的文字會出現在這裡。", "练习听写的文字会出现在这里。"},
	"obPracticed":    {"Your first dictation was inserted.", "第一次聽寫已寫入。", "第一次听写已写入。"},
	"obNeedFirst":    {"Finish the speech model and the permissions first.", "請先完成語音模型與權限。", "请先完成语音模型与权限。"},
	"obPracticeNote": {"Press Start practice, say a sentence, then stop. The text appears in the field above and nowhere else: no app is written to and the clipboard is not used.", "按「開始練習」，說一句話，然後結束。文字只會出現在上方的輸入框：不會寫入任何 App，也不會使用剪貼簿。", "按“开始练习”，说一句话，然后结束。文字只会出现在上方的输入框：不会写入任何应用，也不会使用剪贴板。"},
	"obStart":        {"Start practice", "開始練習", "开始练习"},
	"obNeedStep":     {"Finish this step to continue, or choose Later.", "完成此步驟後才能繼續，或選擇「稍後」。", "完成此步骤后才能继续，或选择“稍后”。"},
	"obBack":         {"Back", "上一步", "上一步"},
	"obContinue":     {"Continue", "繼續", "继续"},
	"obFinish":       {"Finish", "完成", "完成"},
	"obNotYet":       {"Not done yet", "尚未完成", "尚未完成"},
	"obStepOf":       {"Step %d of %d", "第 %d 步，共 %d 步", "第 %d 步，共 %d 步"},
	"obStDone":       {"done", "已完成", "已完成"},
	"obStTodo":       {"not done", "未完成", "未完成"},
	"obAgain":        {"Try again", "再試一次", "再试一次"},
	"obFix":          {"Go there", "前往", "前往"},
	"obLater":        {"Later", "稍後", "稍后"},
	"obComplete":     {"Voicemore is set up.", "Voicemore 已設定完成。", "Voicemore 已设置完成。"},
	"obCompleteText": {"Press %s in any app to dictate. This guide stays available in Settings → General.", "在任何 App 中按 %s 即可聽寫。此指引可在「設定 → 一般」再次打開。", "在任何应用中按 %s 即可听写。此指引可在“设置 → 通用”中再次打开。"},
	"obReview":       {"Review steps", "查看步驟", "查看步骤"},
	"obEnter":        {"Enter Voicemore", "進入 Voicemore", "进入 Voicemore"},
	"obGuide":        {"Setup guide", "設定指引", "设置指引"},
	"obGuideHint":    {"The getting-started steps, in a window of their own", "獨立視窗的開始使用步驟", "独立窗口的开始使用步骤"},
	"obShowBtn":      {"Open guide", "打開指引", "打开指引"},

	"updateNewShort":       {"Update available", "有可用更新", "有可用更新"},
	"updateDownloadShort":  {"Downloading", "正在下載", "正在下载"},
	"updateVerifyShort":    {"Verifying", "正在驗證", "正在验证"},
	"updateVerifyNote":     {"Download complete. Verifying the update…", "下載完成，正在驗證更新…", "下载完成，正在验证更新…"},
	"updateRestartShort":   {"Restarting", "正在重新啟動", "正在重启"},
	"updateRestartAction":  {"Restart now", "立即重新啟動", "立即重启"},
	"updateRestartWaiting": {"Waiting for current tasks to finish…", "等待目前工作結束…", "等待当前任务结束…"},
	"updateAttention":      {"Update needs attention", "更新遇到問題", "更新遇到问题"},
	"updateAutoNote":       {"Voicemore checks automatically and lets you know when an update is available.", "Voicemore 會自動檢查，有新版本時會在此提醒。", "Voicemore 会自动检查，有新版本时会在此提醒。"},
	"updateReadyRestart":   {"Update ready. Restart when convenient.", "更新已就緒，可稍後重新啟動。", "更新已就绪，可稍后重启。"},
	"updatePauseNote":      {"Downloading in the background.", "正在背景下載。", "正在后台下载。"},
	"updateVersion":        {"Current version %s", "目前版本 %s", "当前版本 %s"},
	"updateClose":          {"Close update panel", "關閉更新面板", "关闭更新面板"},
	"updateErrorDetails":   {"Hover for error details", "移至此處查看錯誤詳情", "悬停查看错误详情"},
	"updateRestartFailed":  {"Update installed. Quit and reopen Voicemore to finish.", "更新已安裝，請退出並重新開啟 Voicemore。", "更新已安装，请退出并重新打开 Voicemore。"},
	"updateCheck":          {"Check for Updates", "檢查更新", "检查更新"},
	"updateInstall":        {"Download update", "下載更新", "下载更新"},
	"updateFound":          {"Version %s available", "有新版本 %s", "发现新版本 %s"},
	"updateChecking":       {"Checking for updates…", "正在檢查更新…", "正在检查更新…"},
	"updateCurrent":        {"You are up to date.", "已是最新版本。", "已是最新版本。"},
	"updateUnavailable":    {"Install the app in a writable Applications folder to enable updates. Development builds cannot update.", "請將應用安裝到可寫入的應用程式資料夾。開發版本不支援更新。", "请将应用安装到可写入的 Applications 文件夹。开发版本不支持更新。"},
	"updateCheckFailed":    {"Could not check for updates. Try again.", "無法檢查更新，請重試。", "无法检查更新，请重试。"},
	"updateBusy":           {"Finish dictation or model setup before updating.", "請在聽寫或模型部署完成後更新。", "请在听写或模型部署完成后更新。"},
	"updateDownloading":    {"Downloading and verifying update…", "正在下載並驗證更新…", "正在下载并验证更新…"},
	"updateProgress":       {"Downloading update: %d%%", "正在下載更新：%d%%", "正在下载更新：%d%%"},
	"updateInstallFailed":  {"Update failed. Your current app is still available; retry below.", "更新失敗，目前版本仍可使用，請重試。", "更新失败，当前版本仍可使用，请重试。"},
	"updateCancelled":      {"Update cancelled.", "已取消更新。", "已取消更新。"},
	"updateRestarting":     {"Update installed. Restarting when current tasks finish…", "更新已安裝，待目前工作完成後重新啟動…", "更新已安装，当前任务完成后将自动重启…"},
	"updateCancel":         {"Cancel Update", "取消更新", "取消更新"},

	"builtinAsr":        {"On-device speech", "本機語音模型", "本地语音模型"},
	"builtinNote":       {"Copy an existing model or download it here. Voicemore manages the runtime and starts it with the app. Works offline after setup.", "複製現有模型或在此下載。Voicemore 管理執行環境，隨軟體啟動，部署後可離線使用。", "复制现有模型或在此下载。Voicemore 管理运行环境，随软件启动，部署后可离线使用。"},
	"builtinSize":       {"%.2f GB", "%.2f GB", "%.2f GB"},
	"builtinSource":     {"Existing model folder", "現有模型資料夾", "现有模型文件夹"},
	"builtinCopy":       {"Copy and deploy", "複製並部署", "复制并部署"},
	"builtinDownload":   {"Download and deploy", "下載並部署", "下载并部署"},
	"builtinStart":      {"Start model", "啟動模型", "启动模型"},
	"builtinStop":       {"Stop model", "停止模型", "停止模型"},
	"builtinAbsent":     {"Not deployed", "尚未部署", "尚未部署"},
	"builtinInstalling": {"Deploying", "正在部署", "正在部署"},
	"builtinStarting":   {"Loading model", "正在載入模型", "正在加载模型"},
	"builtinReady":      {"Ready", "已就緒", "已就绪"},
	"builtinStopping":   {"Stopping", "正在停止", "正在停止"},
	"builtinStopped":    {"Standby · loads on dictation", "待機 · 聽寫時自動載入", "待机 · 听写时自动加载"},
	"builtinIdle":       {"Unloads after 5 idle minutes; automatically loads for dictation. Audio is buffered while loading.", "閒置 5 分鐘後釋放記憶體；聽寫時自動載入，載入期間保留錄音。", "空闲 5 分钟后释放内存；听写时自动加载，加载期间保留录音。"},
	"builtinError":      {"Needs attention", "需要處理", "需要处理"},
	"builtinLocation":   {"Stored locally: %s", "本機儲存：%s", "本机存储：%s"},
	// Navigation and the tray.
	"navDictate":  {"Dictation", "聽寫", "听写"},
	"navHistory":  {"History", "歷史", "历史"},
	"navDict":     {"Dictionary", "詞典", "词典"},
	"navSettings": {"Settings", "設定", "设置"},
	"trayToggle":  {"Start / Stop Dictation", "開始／結束聽寫", "开始／结束听写"},
	"trayCleanup": {"Tidy Up Text", "整理文字", "整理文字"},
	"trayOpen":    {"Open Voicemore…", "打開 Voicemore…", "打开 Voicemore…"},

	// Startup problems.
	"errDataDir":   {"The data folder cannot be used: %s", "無法使用資料夾：%s", "无法使用数据目录：%s"},
	"errLoad":      {"Could not read %s: %s", "無法讀取%s：%s", "无法读取%s：%s"},
	"fileSettings": {"the settings", "設定", "设置"},
	"fileSecrets":  {"encrypted credentials", "加密憑證", "加密凭证"},
	"fileHistory":  {"the history", "歷史", "历史"},
	"fileDict":     {"the dictionary", "詞典", "词典"},
	"errTrigger":   {"The trigger key is not available: %s", "觸發鍵無法使用：%s", "触发键不可用：%s"},
	"errShortcut":  {"The shortcut %s could not be registered: %s", "無法註冊快速鍵 %s：%s", "无法注册快捷键 %s：%s"},
	"errKeyMigrate": {
		"An API key is still stored as plain text in settings.json because the encrypted local store could not save it: %s",
		"API 金鑰仍以明文存放在 settings.json，因為本機加密儲存失敗：%s",
		"API 密钥仍以明文保存在 settings.json 中，因为本机加密存储失败：%s",
	},
	"errKeyRead": {"The cleanup API key could not be read from the encrypted local store: %s", "無法從本機加密檔案讀取整理用的 API 金鑰：%s", "无法从本机加密文件读取整理用的 API 密钥：%s"},

	// Dictation page and overlay.
	"startIn3":     {"Start dictation in 3 seconds", "3 秒後開始聽寫", "3 秒后开始听写"},
	"startHint":    {"Switch to the input you want to dictate into after clicking. Or press %s in any app.", "點按後請切換到要輸入的位置；也可以在任何 App 中按 %s。", "点击后请切换到要输入的位置；也可以在任何应用中按 %s。"},
	"stopInsert":   {"Stop and insert", "結束並寫入", "结束并写入"},
	"cancel":       {"Cancel", "取消", "取消"},
	"cleanup":      {"Tidy up text", "整理文字", "整理文字"},
	"state":        {"State", "狀態", "状态"},
	"stIdle":       {"Idle", "待命", "待命"},
	"micNoFrames":  {"The microphone did not deliver audio. Check the input device and try again.", "麥克風尚未傳回音訊，請檢查輸入裝置後重試。", "麦克风未传回音频，请检查输入设备后重试。"},
	"stPreparing":  {"Preparing", "準備中", "准备中"},
	"stRecording":  {"Recording", "錄音中", "录音中"},
	"stSilent":     {"Listening — no sound", "聆聽中 — 沒有聲音", "监听中 — 没有声音"},
	"stFinalizing": {"Finalizing", "定稿中", "定稿中"},
	"stCleaning":   {"Tidying up", "整理中", "整理中"},
	"stInserting":  {"Inserting", "寫入中", "写入中"},
	"stDone":       {"Inserted", "已寫入", "已写入"},
	"stFailed":     {"Not inserted", "未寫入", "未写入"},
	"ovCancel":     {"Cancel this dictation", "取消本次聽寫", "取消本次听写"},
	"ovConfirm":    {"Stop recording and insert", "結束錄音並寫入", "结束录音并写入"},
	"ovPreview":    {"Show or hide the transcript preview", "顯示或收起轉寫預覽", "显示或收起转写预览"},
	"noText":       {"No transcript yet", "尚無轉寫文字", "尚无转写文字"},
	"pendingMark":  {"not final: %s", "未定稿：%s", "未定稿：%s"},
	"willClean":    {"This dictation will be tidied up; the original stays in History.", "本次會整理文字；原始轉寫保留在歷史中。", "本次会整理文字；原始转写保留在历史中。"},
	"ovRetained":   {"Text kept, ready to copy", "文字已保留，可複製", "文字已保留，可复制"},
	"copyText":     {"Copy text", "複製文字", "复制文字"},
	"retryClean":   {"Retry cleanup", "重試整理", "重试整理"},
	"insertRaw":    {"Insert original", "寫入原文", "写入原文"},
	"close":        {"Close", "關閉", "关闭"},

	// History.
	"search":          {"Search", "搜尋", "搜索"},
	"noRecords":       {"No records", "沒有記錄", "没有记录"},
	"pickRecord":      {"Select a record to replay it and see what was inserted.", "選擇一筆記錄，即可重播錄音並查看寫入的內容。", "选择一条记录，即可回放录音并查看写入的内容。"},
	"emptyText":       {"(no text)", "（無文字）", "（无文字）"},
	"notInsertedTag":  {"Not inserted", "未寫入", "未写入"},
	"cancelledTag":    {"Cancelled", "已取消", "已取消"},
	"failedTag":       {"Failed", "失敗", "失败"},
	"play":            {"Play", "播放", "播放"},
	"pause":           {"Pause", "暫停", "暂停"},
	"seek":            {"Playback position", "播放進度", "播放进度"},
	"noAudioOff":      {"No audio to replay: saving audio was off for this recording", "沒有可重播的錄音：這次錄音時未開啟錄音保存", "没有可回放的录音：这次录音时未开启录音保存"},
	"noAudioGone":     {"No audio to replay: %s", "沒有可重播的錄音：%s", "没有可回放的录音：%s"},
	"cantPlay":        {"The recording cannot be played: %s", "無法播放錄音：%s", "无法播放录音：%s"},
	"copy":            {"Copy", "複製", "复制"},
	"copyOf":          {"Copy: %s", "複製：%s", "复制：%s"},
	"copied":          {"Copied", "已複製", "已复制"},
	"reviseShort":     {"Revise", "修訂", "修订"},
	"more":            {"More actions", "更多動作", "更多操作"},
	"retranscribe":    {"Transcribe again", "重新轉寫", "重新转写"},
	"reclean":         {"Tidy up again", "重新整理", "重新整理"},
	"del":             {"Delete record and audio", "刪除記錄與錄音", "删除记录与录音"},
	"details":         {"Details", "詳細資料", "详细信息"},
	"raw":             {"Original transcript", "原始轉寫", "原始转写"},
	"corrected":       {"After dictionary and formatting rules", "套用詞典與格式規則後", "应用词典与格式规则后"},
	"cleaned":         {"Tidied text", "整理結果", "整理结果"},
	"insertedText":    {"Inserted text", "實際寫入", "实际写入"},
	"cleanOff":        {"Cleanup was off for this dictation", "本次未開啟文字整理", "本次未开启文字整理"},
	"cleanFailedWhy":  {"%s", "%s", "%s"},
	"nothingInserted": {"Nothing was inserted", "未寫入任何內容", "未写入任何内容"},
	"revision":        {"Your revision", "你的修訂", "你的修订"},
	"rerun":           {"Transcribed again", "重新轉寫", "重新转写"},
	"recleaned":       {"Tidied again", "重新整理", "重新整理"},
	"timing":          {"First word %d ms · stop to final %d ms · cleanup %d ms · final to insert %d ms", "首字 %d ms · 結束至定稿 %d ms · 整理 %d ms · 定稿至寫入 %d ms", "首字 %d ms · 结束至定稿 %d ms · 整理 %d ms · 定稿至写入 %d ms"},
	"reviseHint":      {"Corrections of names and terms become dictionary candidates. Nothing applies until you accept it.", "對專有名詞的更正會成為詞典候選，確認前不會生效。", "对专有名词的纠正会成为词典候选，确认前不会生效。"},
	"restore":         {"Restore original", "還原原文", "恢复原文"},
	"saveRevision":    {"Save revision", "儲存修訂", "保存修订"},
	"revSaved":        {"Revision saved. %d candidate(s) are waiting in Dictionary.", "修訂已儲存。詞典中有 %d 個候選等待確認。", "修订已保存。词典中有 %d 个候选等待确认。"},
	"revSavedNone":    {"Revision saved. No dictionary rule was proposed from this change.", "修訂已儲存。這次改動沒有提出詞典規則。", "修订已保存。这次改动没有提出词典规则。"},
	"retranscribing":  {"Transcribing again…", "重新轉寫中…", "重新转写中…"},
	"recleaning":      {"Tidying up…", "整理中…", "整理中…"},
	"versionAdded":    {"A new version was added; the earlier result is kept.", "已新增一個版本，先前的結果仍保留。", "已添加一个版本，之前的结果仍保留。"},
	"deleted":         {"Record and audio deleted", "已刪除記錄與錄音", "已删除记录与录音"},
	"failed":          {"Failed: %s", "失敗：%s", "失败：%s"},

	// Dictionary.
	"dictTitle":      {"Dictionary Management", "詞典管理", "词典管理"},
	"hotOver":        {"The local dictionary exceeds the recognizer's limits and is not sent: %s", "本機詞典超出辨識服務的限制，不會傳送：%s", "本机词典超出识别服务的限制，不会发送：%s"},
	"pending":        {"Waiting for confirmation (%d)", "等待確認（%d）", "等待确认（%d）"},
	"noPending":      {"Revise a transcript in History and corrections of names appear here. Nothing applies before you accept it.", "在歷史中修訂轉寫後，專有名詞的更正會出現在這裡。確認前不會生效。", "在历史中修订转写后，专有名词的纠正会出现在这里。确认前不会生效。"},
	"candOriginal":   {"Original input: ", "原始輸入：", "原始输入："},
	"candShort":      {"%s · seen %d×", "%s · 出現 %d 次", "%s · 出现 %d 次"},
	"context":        {"Context", "上下文", "上下文"},
	"srcRevision":    {"history revision", "歷史修訂", "历史修订"},
	"srcObserved":    {"edit after insertion", "寫入後的修改", "写入后的修改"},
	"conflict":       {"Conflict: %s", "衝突：%s", "冲突：%s"},
	"commonWord":     {"A replacement rule for “%s” needs context words.", "「%s」的替換規則需要限定上下文詞。", "“%s”的替换规则需要限定上下文词。"},
	"ctxPlaceholder": {"Replace only when the transcript contains… (comma separated)", "僅當轉寫包含這些詞時才替換…（以逗號分隔）", "仅当转写包含这些词时才替换…（用逗号分隔）"},
	"ctxLabel":       {"Context words", "限定上下文詞", "限定上下文词"},
	"acceptHint":     {"Add keyword", "加入關鍵詞", "添加关键词"},
	"acceptRule":     {"Accept as replacement rule", "接受為替換規則", "接受为替换规则"},
	"ignore":         {"Ignore", "忽略", "忽略"},
	"addEntry":       {"Add by hand", "手動新增", "手动添加"},
	"right":          {"Correct word, e.g. Claude", "正確詞彙，例如 Claude", "正确词汇，例如 Claude"},
	"wrong":          {"Heard as, e.g. Cloud", "被聽成，例如 Cloud", "被听成，例如 Cloud"},
	"wrongToggle":    {"Wrong spelling (optional)", "錯誤寫法（選填）", "错误写法（选填）"},
	"willHint":       {"Adds a term hint.", "將新增為詞彙提示。", "将添加为词汇提示。"},
	"willRule":       {"Adds a replacement rule.", "將新增為替換規則。", "将添加为替换规则。"},
	"add":            {"Add", "新增", "添加"},
	"importSeed":     {"Import seed terms", "匯入種子詞表", "导入种子词表"},
	"importHelp":     {"Import a UTF-8 TXT file, one keyword per line. Blank lines and duplicates are skipped.", "匯入 UTF-8 TXT 檔案，每行一個關鍵詞。自動略過空白行和重複詞。", "导入 UTF-8 TXT 文件，每行一个关键词，自动跳过空行和重复词。"},
	"importFile":     {"Import from file…", "從檔案匯入…", "从文件导入…"},
	"exportDict":     {"Export…", "匯出…", "导出…"},
	"imported":       {"%d terms in the file, %d new, %d already present.", "檔案中共 %d 項，新增 %d 項，其餘 %d 項已在詞典中。", "文件中共 %d 项，新增 %d 项，其余 %d 项已在词典中。"},
	"exported":       {"Dictionary exported to %s", "詞典已匯出至 %s", "词典已导出到 %s"},
	"entries":        {"Entries (%d)", "詞條（%d）", "词条（%d）"},
	"termHint":       {"Term hint", "詞彙提示", "词汇提示"},
	"rule":           {"Replacement rule", "替換規則", "替换规则"},
	"onlyWith":       {"only with: %s", "僅當出現：%s", "仅当出现：%s"},
	"remove":         {"Delete %s", "刪除 %s", "删除 %s"},
	"added":          {"Added: %s", "已新增：%s", "已添加：%s"},
	"removed":        {"Deleted: %s", "已刪除：%s", "已删除：%s"},
	"acceptedHint":   {"Keyword added: %s", "已加入關鍵詞：%s", "已添加关键词：%s"},
	"acceptedRule":   {"Rule added: %s → %s", "已加入規則：%s → %s", "已加入规则：%s → %s"},
	"ignored":        {"Ignored. The same correction will not be proposed again.", "已忽略。同樣的更正不會再提出。", "已忽略。同样的纠正不会再提出。"},

	// Settings.
	"gPerms":              {"Permissions", "權限", "权限"},
	"permDragHint":        {"Drag into the list above to allow access.", "拖入上方列表，開啟權限", "拖入上方列表，开启权限"},
	"permDragSide":        {"Drag into the list to allow access.", "拖入列表，開啟權限", "拖入列表，开启权限"},
	"permDragGranted":     {"Access allowed", "已開啟權限", "已开启权限"},
	"permDragUnavailable": {"Open the installed app to drag it here.", "請開啟已安裝的應用程式以拖移授權。", "请打开已安装的应用以拖拽授权。"},
	"permDragApp":         {"Drag Voicemore to System Settings", "拖移 Voicemore 到系統設定", "拖拽 Voicemore 到系统设置"},
	"permDragFailed":      {"Could not drag the app. Try again.", "無法拖移應用程式，請重試。", "无法拖拽应用，请重试。"},

	"gGeneral":          {"General", "一般", "通用"},
	"gTriggerClean":     {"Trigger and text cleanup", "觸發與文字整理", "触发与文字整理"},
	"gModels":           {"Models", "模型", "模型"},
	"gPrivacy":          {"Privacy", "隱私", "隐私"},
	"pMic":              {"Microphone", "麥克風", "麦克风"},
	"pAx":               {"Accessibility (insert text)", "輔助使用（寫入文字）", "辅助功能（写入文字）"},
	"pInput":            {"Input Monitoring (trigger key)", "輸入監控（觸發鍵）", "输入监控（触发键）"},
	"granted":           {"Granted", "已授權", "已授权"},
	"notGranted":        {"Not granted", "未授權", "未授权"},
	"openSettings":      {"Open System Settings", "打開系統設定", "打开系统设置"},
	"request":           {"Request", "請求授權", "请求授权"},
	"permHint":          {"Restart Voicemore after granting Accessibility or Input Monitoring.", "授予輔助使用或輸入監控後，請重新啟動 Voicemore。", "授予辅助功能或输入监控后，请重新启动 Voicemore。"},
	"language":          {"Language", "語言", "语言"},
	"sKey":              {"Trigger key", "觸發鍵", "触发键"},
	"sKeyHint":          {"Works while any app is in front", "任何 App 在前景時都可使用", "任何应用在前台时都可使用"},
	"sMode":             {"Trigger mode", "觸發方式", "触发方式"},
	"sModeHint":         {"Toggle: press once to start, once more to stop and insert", "切換：按一次開始，再按一次結束並寫入", "切换：按一次开始，再按一次结束并写入"},
	"mToggle":           {"Toggle", "按一次切換", "按一次切换"},
	"mHold":             {"Hold to talk", "按住說話", "按住说话"},
	"rOption":           {"Right Option", "右 Option", "右 Option"},
	"rCommand":          {"Right Command", "右 Command", "右 Command"},
	"rControl":          {"Right Control", "右 Control", "右 Control"},
	"customKey":         {"Custom shortcut: %s", "自訂快速鍵：%s", "自定义快捷键：%s"},
	"sCleanDefault":     {"Tidy up by default", "預設整理文字", "默认整理文字"},
	"sCleanDefaultHint": {"Off inserts the transcript exactly as recognized", "關閉時會原樣寫入辨識結果", "关闭时会原样写入识别结果"},
	"sAudio":            {"Save audio", "保存錄音", "保存录音"},
	"sAudioHint":        {"Off keeps text only, with no replay", "關閉後只保留文字，無法重播", "关闭后只保留文字，无法回放"},
	"sDays":             {"Keep audio for (days)", "錄音保留天數", "录音保留天数"},
	"sDaysHint":         {"Expired audio is deleted; text stays. 0 keeps it", "到期會刪除錄音，文字保留。0 表示不自動刪除", "到期会删除录音，文字保留。0 表示不自动删除"},
	"sObserve":          {"Learn from edits after insertion", "從寫入後的修改學習", "从写入后的修改学习"},
	"sObserveHint":      {"Watches corrections until send or timeout; ignores appended text. Suggestions need confirmation. Off by default", "持續觀察修正至傳送或逾時，忽略追加文字；候選詞需確認。預設關閉", "持续观察纠正至发送或超时，忽略追加文字；候选词需确认。默认关闭"},
	"clearAll":          {"Clear all history and audio…", "清除全部歷史與錄音…", "清空全部历史与录音…"},
	"clearAsk":          {"Clear all history?", "要清除全部歷史嗎？", "要清空全部历史吗？"},
	"clearDetail":       {"Every record and recording is deleted. This cannot be undone. The dictionary is not affected.", "所有記錄與錄音都會刪除，無法還原。詞典不受影響。", "所有记录与录音都会删除，无法撤销。词典不受影响。"},
	"clearYes":          {"Clear", "清除", "清空"},
	"cleared":           {"History and audio cleared", "歷史與錄音已清除", "历史与录音已清空"},
	"notSaved":          {"The settings could not be saved: %s", "無法儲存設定：%s", "无法保存设置：%s"},

	// Models.
	"mAsr":          {"Speech recognition", "語音辨識", "语音识别"},
	"mCleanup":      {"Text cleanup", "文字整理", "文字整理"},
	"mNoModel":      {"Not set", "未設定", "未设置"},
	"mUrl":          {"URL", "網址", "地址"},
	"mModelId":      {"Model ID", "模型 ID", "模型 ID"},
	"mModel":        {"Model", "模型", "模型"},
	"mKey":          {"API key", "API 金鑰", "API 密钥"},
	"mKeyHint":      {"Optional. Encrypted and saved locally, never in settings.json", "選填。加密儲存在本機，不會寫入 settings.json", "选填。加密保存在本机，不会写入 settings.json"},
	"mKeyNone":      {"None", "無", "无"},
	"mKeyStored":    {"A key is stored. Type to replace it", "已儲存金鑰。輸入即可取代", "已保存密钥。输入即可替换"},
	"saveKey":       {"Save key", "儲存金鑰", "保存密钥"},
	"showKey":       {"Show API key", "顯示 API 金鑰", "显示 API 密钥"},
	"hideKey":       {"Hide API key", "隱藏 API 金鑰", "隐藏 API 密钥"},
	"clearKey":      {"Remove the stored API key", "移除已儲存的 API 金鑰", "移除已保存的 API 密钥"},
	"keySaved":      {"API key encrypted and saved locally", "API 金鑰已加密儲存在本機", "API 密钥已加密保存在本机"},
	"keyCleared":    {"API key removed", "已移除 API 金鑰", "已移除 API 密钥"},
	"keyNotSaved":   {"The key was not stored and is kept for this session only: %s", "金鑰未能儲存，僅在本次工作階段保留：%s", "密钥未能保存，仅在本次会话中保留：%s"},
	"helpAsr":       {"Used for dictation only. This service has its own streaming protocol; it is not an OpenAI-compatible endpoint, takes no API key, and is set up separately from text cleanup. It loads its model itself: the model shown is what it reports, and cannot be changed from here.", "僅用於聽寫。此服務使用自己的串流協定，不是 OpenAI 相容端點，不需要 API 金鑰，並與文字整理分開設定。模型由服務自行載入：這裡顯示的是它回報的模型，無法在此更改。", "仅用于听写。此服务使用自己的流式协议，不是 OpenAI 兼容端点，不需要 API 密钥，并与文字整理分开配置。模型由服务自行加载：这里显示的是它报告的模型，无法在此更改。"},
	"helpClean":     {"Used for tidy up, retry and tidy up again only, and only when cleanup is on. An OpenAI-compatible endpoint, separate from speech recognition.", "僅用於整理、重試與重新整理，且只在開啟整理時使用。這是 OpenAI 相容端點，與語音辨識是不同的服務。", "仅用于整理、重试与重新整理，且只在开启整理时使用。这是 OpenAI 兼容端点，与语音识别是不同的服务。"},
	"fetchModels":   {"Fetch models", "取得模型", "获取模型"},
	"testConn":      {"Test connection", "測試連線", "测试连接"},
	"checkAsr":      {"Check service", "檢查服務", "检查服务"},
	"fetching":      {"Fetching…", "取得中…", "获取中…"},
	"testing":       {"Testing…", "測試中…", "测试中…"},
	"mAll":          {"Models on this service (%d)", "此服務上的模型（%d）", "此服务上的模型（%d）"},
	"mInUse":        {"in use", "使用中", "使用中"},
	"kEmbedding":    {"Embedding", "嵌入", "嵌入"},
	"kReranker":     {"Reranker", "重排序", "重排序"},
	"kSpeech":       {"Speech", "語音", "语音"},
	"kDraft":        {"Draft", "草稿", "草稿"},
	"kOther":        {"Other", "其他", "其他"},
	"kGuess":        {"Kinds are guessed from the model names.", "類別是依模型名稱推測的。", "类别是根据模型名称推测的。"},
	"fetchOk":       {"The service lists %d models.", "此服務列出 %d 個模型。", "此服务列出 %d 个模型。"},
	"testOk":        {"The service answered and lists %s. No text was sent, so this does not prove the model can tidy text.", "服務有回應，且清單中有 %s。未傳送任何文字，因此無法證明此模型能整理文字。", "服务有响应，且列表中有 %s。未发送任何文字，因此不能证明此模型能整理文字。"},
	"testNotListed": {"The service answered, but %s is not among its %d models.", "服務有回應，但 %s 不在它的 %d 個模型之中。", "服务有响应，但 %s 不在它的 %d 个模型之中。"},
	"testNoModel":   {"Enter a model ID first.", "請先輸入模型 ID。", "请先输入模型 ID。"},
	"asrOk":         {"The service answered: %s. Model: %s.", "服務有回應：%s。模型：%s。", "服务有响应：%s。模型：%s。"},
	"asrReady":      {"ready", "就緒", "就绪"},
	"asrBusyNow":    {"busy with a recording", "正在處理錄音", "正在处理录音"},
	"asrNotReady":   {"not ready yet", "尚未就緒", "尚未就绪"},
	"asrNoName":     {"not reported", "未回報", "未报告"},

	// Home, the rail and paging.
	"navHome":    {"Home", "首頁", "首页"},
	"homeReady":  {"Press %s to dictate", "按 %s 開始聽寫", "按 %s 开始听写"},
	"homeToggle": {"Press it again to insert the text at your cursor.", "再按一次，文字就會寫入游標所在位置。", "再按一次，文字就会写入光标所在位置。"},
	"homeHold":   {"Hold it while you speak; release to insert the text at your cursor.", "說話時按住，放開後文字會寫入游標所在位置。", "说话时按住，松开后文字会写入光标所在位置。"},
	"homeSetup":  {"Setup", "設定概覽", "设置概览"},
	"hShortcut":  {"Shortcut", "快速鍵", "快捷键"},
	"off":        {"Off", "關", "关"},
	"openIn":     {"Open %s in Settings", "在設定中打開「%s」", "在设置中打开“%s”"},
	"homeRecent": {"Recent", "最近", "最近"},
	"showAll":    {"Show all", "顯示全部", "显示全部"},
	"homeEmpty":  {"Your dictations will appear here.", "你的聽寫會出現在這裡。", "你的听写会出现在这里。"},
	"pagePrev":   {"Previous page", "上一頁", "上一页"},
	"pageNext":   {"Next page", "下一頁", "下一页"},
	"pageOf":     {"Page %d of %d", "第 %d / %d 頁", "第 %d / %d 页"},

	// Models: defaults and providers.
	"defaultModels":                 {"Default models", "預設模型", "默认模型"},
	"manage":                        {"Manage", "管理", "管理"},
	"notSet":                        {"Not set", "未設定", "未设置"},
	"unconfigured":                  {"%s — not configured", "%s — 尚未設定", "%s — 尚未配置"},
	"noEligible":                    {"No model is available yet. Set up a provider below.", "目前沒有可用的模型。請先在下方設定供應商。", "目前没有可用的模型。请先在下方配置服务商。"},
	"gAsrProv":                      {"Speech recognition providers", "語音辨識供應商", "语音识别服务商"},
	"gLlmProv":                      {"Language model providers", "語言模型供應商", "语言模型服务商"},
	"addProvider":                   {"Add provider", "新增供應商", "添加服务商"},
	"providerOf":                    {"%s settings", "%s 設定", "%s 设置"},
	"pAliyun":                       {"Alibaba Cloud", "阿里雲", "阿里云"},
	"pVolc":                         {"Volcengine", "火山引擎", "火山引擎"},
	"pDeepseek":                     {"DeepSeek", "DeepSeek", "DeepSeek"},
	"pSub2API":                      {"Voicemore Official", "Voicemore Official", "Voicemore Official"},
	"sub2apiASR":                    {"ASR 2.0 · Official", "ASR 2.0 · Official", "ASR 2.0 · Official"},
	"sub2apiCleanup":                {"DeepSeek · Official", "DeepSeek · Official", "DeepSeek · Official"},
	"recoverVoiceResult":            {"Recover cloud result", "取回雲端結果", "取回云端结果"},
	"sub2apiModel":                  {"ASR 2.0 + DeepSeek", "ASR 2.0 + DeepSeek", "ASR 2.0 + DeepSeek"},
	"managedTranslationUnavailable": {"Voicemore Official does not support translation.", "Voicemore Official 尚不支援翻譯。", "Voicemore Official 尚不支持翻译。"},
	"volcPreset":                    {"Doubao streaming ASR", "豆包流式語音辨識", "豆包流式语音识别"},
	"stOffLong":                     {"Not enabled", "未啟用", "未启用"},
	"stConfigured":                  {"Configured", "已設定", "已配置"},
	"stNeedsKey":                    {"Needs credentials", "需要憑證", "需要凭证"},
	"stNeedsSetup":                  {"Needs setup", "需要設定", "需要设置"},
	"stUnavailable":                 {"Mode not supported", "不支援此接入方式", "不支持此接入方式"},
	"configure":                     {"Configure", "設定", "配置"},
	"nModels":                       {"%d models", "%d 個模型", "%d 个模型"},
	"localTag":                      {"Local", "本機", "本机"},
	"customTag":                     {"Custom", "自訂", "自定义"},
	"enabled":                       {"Enabled", "啟用", "启用"},
	"appId":                         {"App ID", "App ID", "App ID"},
	"accessToken":                   {"Access Token", "Access Token", "Access Token"},
	"getKey":                        {"Get a key", "取得金鑰", "获取密钥"},
	"docs":                          {"Documentation", "說明文件", "文档"},
	"recommended":                   {"Recommended", "推薦", "推荐"},
	"custom":                        {"Added by you", "自行新增", "自行添加"},
	"setDefault":                    {"Set as default", "設為預設", "设为默认"},
	"isDefault":                     {"Default", "預設", "默认"},
	"volcMode":                      {"Access mode", "接入方式", "接入方式"},
	"vNew":                          {"New", "新版", "新版"},
	"vLegacy":                       {"Legacy", "舊版", "旧版"},
	"vAgent":                        {"Agent Plan", "Agent Plan", "Agent Plan"},
	"endpoint":                      {"Endpoint", "端點", "端点"},
	"sendsTo":                       {"Requests go to %s", "請求將送往 %s", "请求将发送到 %s"},
	"addModel":                      {"Add model", "新增模型", "添加模型"},
	"addModelId":                    {"Add a model ID", "新增模型 ID", "添加模型 ID"},
	"errEmptyModel":                 {"Enter a model ID.", "請輸入模型 ID。", "请输入模型 ID。"},
	"errDupModel":                   {"This model is already listed.", "此模型已在清單中。", "此模型已在列表中。"},
	"removeModel":                   {"Remove %s", "移除 %s", "移除 %s"},
	"refNote":                       {"These are the models the provider's setup offers; whether the service serves them is not checked here.", "這些是供應商設定畫面提供的模型；此處不會檢查服務是否提供。", "这些是服务商设置界面提供的模型；此处不会检查服务是否提供。"},
	"provName":                      {"Name", "名稱", "名称"},
	"editProvider":                  {"Edit provider", "編輯供應商", "编辑服务商"},
	"removeProvider":                {"Remove provider", "移除供應商", "移除服务商"},
	"save":                          {"Save", "儲存", "保存"},
	"cancelBtn":                     {"Cancel", "取消", "取消"},
	"errName":                       {"Enter a name.", "請輸入名稱。", "请输入名称。"},
	"keyOptional":                   {"API key (optional)", "API 金鑰（選填）", "API 密钥（选填）"},
	"noModelsYet":                   {"No models yet. Add at least one to use this provider.", "尚無模型。至少新增一個才能使用此供應商。", "尚无模型。至少添加一个才能使用此服务商。"},
	"providerSaved":                 {"%s saved", "已儲存 %s", "已保存 %s"},
	"providerRemoved":               {"%s removed", "已移除 %s", "已移除 %s"},
	"assignCleared":                 {"%s is no longer the default. Nothing was chosen in its place.", "%s 不再是預設，且未自動改選其他模型。", "%s 不再是默认，且未自动改选其他模型。"},
	"asrUnavailable": {
		"Speech recognition through %s is not available in this version: its protocol has no adapter yet. Credentials can be saved here, but it cannot be made the default or tested.",
		"此版本尚不支援透過 %s 進行語音辨識：尚未實作其協定。可以在此儲存憑證，但無法設為預設，也無法測試。",
		"此版本尚不支持通过 %s 进行语音识别：尚未实现其协议。可以在此保存凭证，但无法设为默认，也无法测试。",
	},
	"customAsrNote": {
		"A speech service added here must speak the same streaming protocol as the local speech service (GET /health and its WebSocket). Other protocols are not supported.",
		"在此新增的語音服務必須使用與本機語音服務相同的串流協定（GET /health 與其 WebSocket）。不支援其他協定。",
		"在此添加的语音服务必须使用与本机语音服务相同的流式协议（GET /health 与其 WebSocket）。不支持其他协议。",
	},
	"customLlmNote": {
		"An OpenAI-compatible API: the address is the base its /chat/completions and /models hang from, usually ending in /v1.",
		"OpenAI 相容的 API：網址是 /chat/completions 與 /models 所在的基礎位址，通常以 /v1 結尾。",
		"OpenAI 兼容的 API：地址是 /chat/completions 与 /models 所在的基础地址，通常以 /v1 结尾。",
	},
	"eNotSet":       {"No default model is set. Choose one in Settings → Models.", "尚未設定預設模型。請在「設定 → 模型」選擇。", "尚未设置默认模型。请在“设置 → 模型”中选择。"},
	"eUnconfigured": {"The default model's provider is not configured. Check it in Settings → Models.", "預設模型的供應商尚未設定。請在「設定 → 模型」檢查。", "默认模型的服务商尚未配置。请在“设置 → 模型”中检查。"},
	"eNoAdapter":    {"The default model, or its provider's access mode, is not supported in this version. Choose another in Settings → Models.", "此版本尚不支援預設模型或其供應商的接入方式。請在「設定 → 模型」改選。", "此版本尚不支持默认模型或其服务商的接入方式。请在“设置 → 模型”中改选。"},

	// Cloud speech recognition.
	"whyAgentPlan":      {"No official document was found for an Agent Plan key on the streaming recognition API, so this mode cannot be used yet. Use New or Legacy.", "找不到 Agent Plan 金鑰用於串流辨識 API 的官方文件，因此暫時無法使用此方式。請改用新版或舊版。", "未找到 Agent Plan 密钥用于流式识别 API 的官方文档，因此暂时无法使用此方式。请改用新版或旧版。"},
	"whyNoProtocol":     {"Not in the provider's realtime model list; not offered", "不在供應商的即時模型清單中，暫不提供", "不在服务商的实时模型列表中，暂不提供"},
	"aliyunBaseNote":    {"The default still works; Alibaba recommends your workspace's own address, wss://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/api-ws/v1.", "預設位址仍可使用；阿里雲建議改用業務空間專屬位址 wss://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/api-ws/v1。", "默认地址仍可使用；阿里云建议改用业务空间专属地址 wss://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/api-ws/v1。"},
	"eBadSocketURL":     {"The address is not a valid ws(s) address.", "位址不是有效的 ws(s) 位址。", "地址不是有效的 ws(s) 地址。"},
	"volcResource":      {"Billing", "計費方式", "计费方式"},
	"vHourly":           {"Hourly", "小時版", "小时版"},
	"vConcurrent":       {"Concurrent", "並發版", "并发版"},
	"vClassicHourly":    {"Classic hourly", "經典小時版", "经典小时版"},
	"probeOk":           {"The provider accepted the credentials and started a session for %s. No audio was sent, so recognition itself is not tested.", "供應商已接受憑證，並為 %s 開始了工作階段。未傳送任何音訊，因此並未測試辨識本身。", "服务商已接受凭证，并为 %s 开始了会话。未发送任何音频，因此并未测试识别本身。"},
	"eCloudAuth":        {"The provider refused the credentials. Check them, and that they belong to this access mode.", "供應商拒絕了憑證。請檢查憑證，以及是否屬於所選的接入方式。", "服务商拒绝了凭证。请检查凭证，以及是否属于所选的接入方式。"},
	"eCloudUnreachable": {"The provider cannot be reached: %s", "無法連上供應商：%s", "无法连接服务商：%s"},
	"eCloudProtocol":    {"The provider answered in an unexpected way: %s", "供應商的回應不符預期：%s", "服务商的响应不符合预期：%s"},
	"eCloudService":     {"The provider reported an error (%s): %s", "供應商回報錯誤（%s）：%s", "服务商报告错误（%s）：%s"},
	"hotwordVolc":       {"Doubao prioritizes recent terms in live preview (100 tokens) and uses the submitted dictionary in the final second pass. Hints do not force replacements.", "豆包即時預覽優先使用新詞（100 tokens），最終二遍辨識使用提交的詞典；熱詞不保證強制替換。", "豆包实时预览优先使用新词（100 tokens），最终二遍识别使用提交的词典；热词不保证强制替换。"},
	"hotwordCloud":      {"This recognizer does not support dictionary term hints.", "目前辨識服務不支援詞典詞彙提示。", "当前识别服务不支持词典词汇提示。"},

	// Errors of the model services.
	"eNotConfigured":    {"Text cleanup is not set up: set its URL and model in Settings → Models → Text cleanup.", "尚未設定文字整理：請在「設定 → 模型 → 文字整理」填寫網址與模型。", "尚未配置文字整理：请在“设置 → 模型 → 文字整理”中填写地址与模型。"},
	"eBadURL":           {"The URL is not a valid http(s) address.", "網址不是有效的 http(s) 位址。", "地址不是有效的 http(s) 地址。"},
	"eTimeout":          {"The service did not answer in time.", "服務逾時未回應。", "服务超时未响应。"},
	"eCancelled":        {"Cancelled.", "已取消。", "已取消。"},
	"eUnreachable":      {"The service cannot be reached: %s", "無法連上服務：%s", "无法连接服务：%s"},
	"eCleanupAuth":      {"Text cleanup model %s: API key missing or invalid (401). Check its key in Settings → Models → Text cleanup.", "文字整理模型 %s 的密鑰缺失或無效（401）。請到設定 → 模型 → 文字整理檢查密鑰。", "文字整理模型 %s 的密钥缺失或无效（401）。请到设置 → 模型 → 文字整理检查密钥。"},
	"eCleanupForbidden": {"Text cleanup model %s: access denied (403). Check model permissions and service access in Settings → Models → Text cleanup.", "文字整理模型 %s 沒有存取權限（403）。請在設定 → 模型 → 文字整理檢查模型權限與服務開通狀態。", "文字整理模型 %s 没有访问权限（403）。请在设置 → 模型 → 文字整理检查模型权限与服务开通状态。"},
	"eAuth":             {"The service refused the request (%s). Check the API key.", "服務拒絕了請求（%s）。請檢查 API 金鑰。", "服务拒绝了请求（%s）。请检查 API 密钥。"},
	"eNotFound":         {"The service has no such path (404). The URL should end where its API begins, usually “/v1”.", "服務沒有這個路徑（404）。網址應止於 API 的起點，通常是「/v1」。", "服务没有这个路径（404）。地址应止于 API 的起点，通常是“/v1”。"},
	"eStatus":           {"The service answered %s.", "服務回應 %s。", "服务返回 %s。"},
	"eNoText":           {"The service answered, but not with anything usable. Is this an OpenAI-compatible API?", "服務有回應，但內容無法使用。這是 OpenAI 相容的 API 嗎？", "整理服务已响应，但未返回有效文字。请重试或检查模型配置。"},
	"eNotASR":           {"The address answered, but not as the speech recognition service does. The cleanup server is a different service.", "此位址有回應，但不是語音辨識服務的回應方式。整理用的伺服器是另一個服務。", "此地址有响应，但不是语音识别服务的响应方式。整理用的服务器是另一个服务。"},

	// Messages of the session controller (internal/session/messages.go).
	"micDenied":            {"Microphone access is denied: allow Voicemore in System Settings → Privacy & Security → Microphone.", "麥克風權限遭拒：請在「系統設定 → 隱私權與安全性 → 麥克風」允許 Voicemore。", "麦克风权限被拒绝：请在“系统设置 → 隐私与安全性 → 麦克风”中允许 Voicemore。"},
	"micNotGranted":        {"Microphone access was not granted, so nothing can be recorded.", "未取得麥克風權限，無法錄音。", "未获得麦克风权限，无法录音。"},
	"micMissing":           {"No microphone found. Connect one and try again.", "找不到麥克風，請連接後重試。", "未找到麦克风，请连接后重试。"},
	"endedEarly":           {"The recording ended before it started.", "錄音在開始前就結束了。", "录音在开始前就结束了。"},
	"asrBusy":              {"The speech recognition service is busy with another recording. Try again in a moment.", "語音辨識服務正在處理另一段錄音，請稍後再試。", "语音识别服务正在处理另一段录音，请稍后再试。"},
	"asrUnreachable":       {"The speech recognition service at %s cannot be reached: %s", "無法連上位於 %s 的語音辨識服務：%s", "无法连接位于 %s 的语音识别服务：%s"},
	"noSpeech":             {"No speech detected — nothing inserted.", "未偵測到語音，未寫入。", "未检测到语音，未写入。"},
	"cleanupRequestFailed": {"Cleanup service request failed: %s", "整理服務請求失敗：%s", "整理服务请求失败：%s"},
	"cleanupTimedOut":      {"Cleanup request timed out: %s", "整理請求逾時：%s", "整理请求超时：%s"},
	"cleanupCancelled":     {"Cleanup cancelled: %s", "整理已取消：%s", "整理已取消：%s"},
	"cleanupFailed":        {"Cleanup failed: %s", "整理失敗：%s", "整理失败：%s"},
	"notInserted":          {"Not inserted: %s", "未寫入：%s", "未写入：%s"},
	"copiedFallback":       {"Text saved to clipboard — press Cmd+V to paste.", "文字已存入剪貼簿，可按 ⌘V 貼上。", "文字已保存到剪贴板，可按 ⌘V 粘贴。"},
	"hotwordNotSent":       {"The dictionary was not sent to the recognizer; its default terms were used: %s", "詞典未傳送給辨識服務，使用其預設詞表：%s", "词典未发送给识别服务，使用其默认词表：%s"},
	"hotwordEmpty":         {"The local dictionary is empty; the recognizer's default terms were used.", "本機詞典是空的，使用辨識服務的預設詞表。", "本机词典为空，使用识别服务的默认词表。"},
	"hotwordSent":          {"Sent %d dictionary terms to the recognizer.", "已向辨識服務傳送本機詞典 %d 項。", "已向识别服务发送本机词典 %d 项。"},
	"hotwordDefault":       {"recognizer's default terms", "辨識服務預設詞表", "识别服务默认词表"},
	"hotwordLocal":         {"%d dictionary terms", "本機詞典 %d 項", "本机词典 %d 项"},
	"noteRules":            {"rules: %s", "規則：%s", "规则：%s"},
	"noteIncomplete":       {"incomplete: %s", "未完成：%s", "未完成：%s"},
	"observeNoRead":        {"Edits after insertion could not be observed: the target app does not let this input be read back. Revise in History instead.", "無法觀察寫入後的修改：目標 App 不允許讀回該輸入框。可以在歷史中修訂。", "无法观察写入后的修改：目标应用不允许读回该输入框。可以在历史中修订。"},
	"observeOutside":       {"Observation ended: the input changed outside the inserted text; nothing was learned.", "觀察結束：輸入框在寫入內容之外有變動，未學習。", "观察结束：输入框在写入内容之外发生变化，未学习。"},
	"observeSame":          {"Observation ended: the inserted text was not changed.", "觀察結束：寫入的內容沒有被修改。", "观察结束：写入的内容没有被修改。"},
	"observeLearned":       {"An edit after insertion was observed: %s", "觀察到寫入後的修改：%s", "观察到写入后的修改：%s"},
	"revisionSame":         {"The revision is the same as the original.", "修訂內容與原文相同。", "修订内容与原文相同。"},
	"recordGone":           {"The record no longer exists.", "記錄已不存在。", "记录已不存在。"},
	"noAudio":              {"This record has no audio to use.", "這筆記錄沒有可用的錄音。", "这条记录没有可用的录音。"},
	"busy":                 {"A recording or transcription is in progress. Try again in a moment.", "正在錄音或轉寫，請稍後再試。", "正在录音或转写，请稍后再试。"},
	"detailPasted":         {"pasted into %s", "已貼入 %s", "已粘贴到 %s"},
	"detailPractice":       {"practice in the setup guide; not inserted into any app", "設定指引中的練習；未寫入任何 App", "设置指引中的练习；未写入任何应用"},
	"detailNoElement":      {"the app exposed no focused element, so only the app could be checked", "該 App 未提供焦點元素，只能核對 App 本身", "该应用未提供焦点元素，只能核对应用本身"},
	"remoteTitle":          {"Remote desktops", "遠端桌面", "远程桌面"},
	"remoteHelp":           {"Enable clipboard sync, choose a working paste shortcut, then click the remote input before dictating.", "開啟客戶端剪貼簿同步，選擇可用的貼上快捷鍵，聽寫前點選遠端輸入框。", "开启客户端剪贴板同步，选择可用的粘贴快捷键，听写前点击远端输入框。"},
	"sendCopyEdit":         {"Copy edited text (target unchanged)", "複製修改（目標未同步）", "复制修改（目标未同步）"},
	"sendDelay":            {"Send delay", "延遲傳送", "延迟发送"},
	"sendImmediately":      {"Immediately", "立即傳送", "立即发送"},
	"sendFinalText":        {"Final text", "最終文字", "最终文字"},
	"sendCountdown":        {"Send in %.1f s", "%.1f 秒後傳送", "%.1f 秒后发送"},
	"sendCancel":           {"Cancel auto-send", "取消自動傳送", "取消自动发送"},
	"autoSend":             {"Auto-send", "自動傳送", "自动发送"},
	"sendAfterInput":       {"Send after input", "輸入後自動傳送", "输入后自动发送"},
	"allApps":              {"AI applications", "AI 應用", "AI 应用"},
	"sendInherit":          {"Follow AI applications", "跟隨 AI 應用", "跟随 AI 应用"},
	"sendOff":              {"Do not send", "不傳送", "不发送"},
	"sendShortcut":         {"Send shortcut", "傳送快捷鍵", "发送快捷键"},
	"applications":         {"Applications", "應用", "应用"},
	"addApplication":       {"Add application", "新增應用", "添加应用"},
	"selectEmpty":          {"No available options", "沒有可選項目", "暂无可选项"},
	"detailSendTriggered":  {"send shortcut triggered: %s", "已觸發傳送快捷鍵：%s", "已触发发送快捷键：%s"},
	"detailSendSkipped":    {"auto-send skipped", "已略過自動傳送", "已跳过自动发送"},
	"remoteOff":            {"Off", "關閉", "关闭"},
	"remoteShortcut":       {"Paste shortcut", "貼上快捷鍵", "粘贴快捷键"},
	"remoteRemove":         {"Remove", "移除", "移除"},
	"remoteWindow":         {"Window title contains", "視窗名稱包含", "窗口名称包含"},
	"remoteWindowHint":     {"Empty matches all client windows; use a connection name to limit scope.", "留空套用所有客戶端視窗；填寫連線名稱可限定範圍。", "留空适用所有客户端窗口；填写连接名称可限定范围。"},
	"remoteWait":           {"Clipboard sync wait", "剪貼簿同步等待", "剪贴板同步等待"},
	"remoteRefresh":        {"Refresh apps", "重新整理應用程式", "刷新应用"},
	"remoteAdd":            {"Add running client…", "加入正在執行的客戶端…", "添加已打开的客户端…"},
	"detailRemote":         {"paste shortcut sent to %s; remote insertion is unverified; clipboard kept for synchronization", "已向 %s 傳送貼上快捷鍵；尚未確認遠端輸入；剪貼簿已保留以供同步", "已向 %s 发送粘贴快捷键；尚未确认远端输入；剪贴板已保留用于同步"},
	"detailClipboard":      {"the clipboard changed meanwhile and was left as it is", "剪貼簿在此期間有變動，已保持原狀", "剪贴板在此期间发生变化，已保持原状"},
	"detailCancelled":      {"cancelled before insertion", "寫入前已取消", "写入前已取消"},
	"detailNoSpeech":       {"the recognizer returned no speech", "辨識服務未回傳語音", "识别服务未返回语音"},
	"detailProgress":       {"in progress", "進行中", "进行中"},
	"detailCleanup":        {"cleanup failed; the transcript was not inserted yet", "整理失敗；轉寫尚未寫入", "整理失败；转写尚未写入"},
	"detailAudio":          {"the audio could not be saved: %s", "無法保存錄音：%s", "无法保存录音：%s"},

	// What macOS does on the 🌐 key, as internal/macos reports it.
	"fnInput":     {"Change Input Source", "切換輸入方式", "切换输入法"},
	"fnEmoji":     {"Show Emoji & Symbols", "顯示表情符號與符號", "显示表情与符号"},
	"fnDictation": {"Start Dictation", "開始聽寫", "开始听写"},
}
