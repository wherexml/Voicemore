package main

func init() {
	texts["customAdvanced"] = [3]string{"Advanced", "進階設定", "高级设置"}
	texts["thinkingMode"] = [3]string{"Thinking mode", "思考模式", "思考模式"}
	texts["thinkingServer"] = [3]string{"Follow server default", "跟隨服務端", "跟随服务端"}
	texts["thinkingDeepSeek"] = [3]string{"Off · DeepSeek protocol", "關閉 · DeepSeek 協議", "关闭 · DeepSeek 协议"}
	texts["thinkingOmlx"] = [3]string{"Off · oMLX protocol", "關閉 · oMLX 協議", "关闭 · oMLX 协议"}
	texts["thinkingCustom"] = [3]string{"Custom parameters", "自訂參數", "自定义参数"}
	texts["thinkingJSON"] = [3]string{"Request parameters (JSON)", "請求參數（JSON）", "请求参数（JSON）"}
	texts["thinkingTestModel"] = [3]string{"Test model", "測試模型", "测试模型"}
	texts["thinkingTest"] = [3]string{"Test cleanup", "測試整理", "测试整理"}
	texts["thinkingTestOK"] = [3]string{"Text returned · %.2f s", "已返回正文 · %.2f 秒", "已返回正文 · %.2f 秒"}
	texts["errThinkingParams"] = [3]string{"Use a JSON object (up to 16 KB), without credentials or model/messages/stream overrides.", "請填寫 JSON 物件（最多 16 KB），不可含憑證或覆寫 model/messages/stream。", "请填写 JSON 对象（最多 16 KB），不可含凭据或覆盖 model/messages/stream。"}
}
