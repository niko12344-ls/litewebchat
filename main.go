package main

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

type Message struct {
	Username string `json:"username"`
	Color    string `json:"color"`
	Content  string `json:"content"`
	Time     string `json:"time"`
}

// 程序配置结构体
type Config struct {
	ListenPort string `json:"listen_port"` // 监听端口，例 "8080"
}

var (
	clients   = make(map[*websocket.Conn]bool)
	broadcast = make(chan Message)
	history   []Message
	logFile   = "chat_history.json"
	configFile = "config.json"
	cfg        Config
	upgrader = websocket.Upgrader{
		ReadBufferSize:  512,
		WriteBufferSize: 512,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
)

// 加载配置文件，无文件则使用默认8080端口
func loadConfig() {
	data, err := os.ReadFile(configFile)
	if err != nil {
		log.Printf("未找到配置文件 %s，默认使用端口 8080", configFile)
		cfg.ListenPort = "8080"
		return
	}
	err = json.Unmarshal(data, &cfg)
	if err != nil {
		log.Fatal("配置文件解析失败：", err)
	}
	log.Printf("已加载配置，监听端口：%s", cfg.ListenPort)
}

func loadHistory() {
	data, err := os.ReadFile(logFile)
	if err != nil {
		return
	}
	json.Unmarshal(data, &history)
}

func saveHistory() {
	data, _ := json.MarshalIndent(history, "", "  ")
	_ = os.WriteFile(logFile, data, 0644)
}

// 重置页面 /rest 仅清除客户端登录缓存，不操作聊天记录
func restPage(w http.ResponseWriter, r *http.Request) {
	html := `
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>清除本地登录信息</title>
<style>
*{margin:0;padding:0;box-sizing:border-box;font-family:monospace}
body{background:#888;color:#fff;padding:20px}
.box{background:#222;padding:30px;border-radius:4px;max-width:400px;margin:50px auto}
button{margin-top:20px;padding:10px 20px;background:#333;color:#fff;border:1px solid #666;font-size:16px}
a{color:#8cf;margin-top:15px;display:inline-block}
</style>
</head>
<body>
<div class="box">
<h2>清除浏览器本地登录缓存</h2>
<p>仅删除本机保存的用户名、文字颜色，<b>不会清空服务端聊天记录</b></p>
<button onclick="clearLocalLogin()">确认清除本地登录信息</button>
<div id="tip"></div>
<a href="/">返回聊天室</a>
<script>
function clearLocalLogin(){
	localStorage.removeItem("chat_username");
	localStorage.removeItem("chat_color");
	fetch("/rest/log",{method:"POST"}).then(res=>res.text()).then(txt=>{
		document.getElementById("tip").innerText = txt;
	})
}
</script>
</div>
</body>
`
	tpl, _ := template.New("rest").Parse(html)
	tpl.Execute(w, nil)
}

// 日志接收接口，仅打印操作，不改动任何数据
func logClearHandle(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "仅支持POST请求", http.StatusMethodNotAllowed)
		return
	}
	log.Printf("[%s] 客户端请求清除本地登录缓存，聊天记录未变动", time.Now().Format("15:04:05"))
	w.Write([]byte("本地登录缓存已清除，刷新聊天室重新登录"))
}

func indexPage(w http.ResponseWriter, r *http.Request) {
	htmlTemplate := `
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no">
<title>聊天室</title>
<style>
*{
	margin:0;
	padding:0;
	font-family:monospace;
	box-sizing:border-box;
}
body{
	padding:8px;
	background:#888888;
	color:#eeeeee;
	min-height:100vh;
}
#login{
	position:fixed;
	top:35%;
	left:50%;
	transform:translate(-50%,-35%);
	border:1px solid #111;
	padding:16px;
	background:#222222;
	width:90%;
	max-width:360px;
	z-index:99;
}
#chatbox{
	width:100%;
	height:72vh;
	border:1px solid #111;
	padding:6px;
	overflow-y:auto;
	margin-bottom:8px;
	background:#222222;
}
.msg{
	margin:4px 0;
	padding:4px 6px;
	background:#333333;
	line-height:1.4;
	border-radius:2px;
}
#input-area{
	display:flex;
	gap:6px;
	width:100%;
}
#msgInput{
	flex:1;
	border:1px solid #111;
	padding:10px;
	font-size:16px;
	background:#222222;
	color:#eee;
}
button{
	border:1px solid #111;
	padding:0 16px;
	cursor:pointer;
	background:#222222;
	color:#eee;
	font-size:16px;
}
input{
	border:1px solid #111;
	padding:8px;
	margin:8px 0;
	width:100%;
	background:#333333;
	color:#eee;
	font-size:16px;
}
</style>
</head>
<body>
<div id="login">
<div>用户名：</div>
<input id="name" placeholder="必填">
<div>文字Hex颜色（默认#ffffff）：</div>
<input id="hex" placeholder="#ffffff">
<br>
<button onclick="enterChat()">进入聊天室</button>
</div>

<div id="chatbox"></div>
<div id="input-area">
	<input id="msgInput" placeholder="输入消息，回车发送" onkeydown="if(event.key === 'Enter')sendMsg()">
	<button onclick="sendMsg()">发送</button>
</div>

<script>
let wsClient;
let userName = "";
let textColor = "#ffffff";

window.onload = function(){
	const saveName = localStorage.getItem("chat_username");
	const saveColor = localStorage.getItem("chat_color");
	if(saveName && saveName.trim() !== ""){
		userName = saveName;
		textColor = saveColor || "#ffffff";
		document.getElementById("login").style.display = "none";
		connectSocket();
	}else{
		document.getElementById("login").style.display = "block";
	}
}

function enterChat() {
	const nameDom = document.getElementById("name");
	const hexDom = document.getElementById("hex");
	userName = nameDom.value.trim();
	const inputHex = hexDom.value.trim();

	if (userName === "") {
		alert("用户名不能为空");
		return;
	}
	if (inputHex !== "") textColor = inputHex;
	localStorage.setItem("chat_username", userName);
	localStorage.setItem("chat_color", textColor);

	document.getElementById("login").style.display = "none";
	connectSocket();
}

function connectSocket() {
	const protocol = window.location.protocol === "https:" ? "wss://" : "ws://";
	const wsUrl = protocol + window.location.host + "/ws";
	wsClient = new WebSocket(wsUrl);

	wsClient.onmessage = function (event) {
		const msgData = JSON.parse(event.data);
		if(Array.isArray(msgData)){
			const chatBox = document.getElementById("chatbox");
			msgData.forEach(item => {
				const lineDom = document.createElement("div");
				lineDom.className = "msg";
				lineDom.style.color = item.color;
				lineDom.textContent = "[" + item.time + "] " + item.username + ": " + item.content;
				chatBox.appendChild(lineDom);
			})
			chatBox.scrollTop = chatBox.scrollHeight;
			return;
		}
		const chatBox = document.getElementById("chatbox");
		const lineDom = document.createElement("div");
		lineDom.className = "msg";
		lineDom.style.color = msgData.color;
		lineDom.textContent = "[" + msgData.time + "] " + msgData.username + ": " + msgData.content;
		chatBox.appendChild(lineDom);
		chatBox.scrollTop = chatBox.scrollHeight;
	};
}

function sendMsg() {
	const inputDom = document.getElementById("msgInput");
	const sendText = inputDom.value.trim();
	if (sendText === "" || !wsClient) return;

	const sendData = {
		username: userName,
		color: textColor,
		content: sendText
	};
	wsClient.send(JSON.stringify(sendData));
	inputDom.value = "";
}
</script>
</body>
`
	tpl, err := template.New("chat").Parse(htmlTemplate)
	if err != nil {
		log.Fatal("模板解析失败：", err)
	}
	err = tpl.Execute(w, nil)
	if err != nil {
		http.Error(w, "页面渲染错误", http.StatusInternalServerError)
	}
}

func wsConnectHandle(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("websocket升级失败：", err)
		return
	}
	defer conn.Close()
	clients[conn] = true

	_ = conn.WriteJSON(history)

	for {
		var msg Message
		err := conn.ReadJSON(&msg)
		if err != nil {
			delete(clients, conn)
			break
		}
		msg.Time = time.Now().Format("15:04:05")
		history = append(history, msg)
		saveHistory()
		broadcast <- msg
	}
}

func broadcastLoop() {
	for msg := range broadcast {
		for client := range clients {
			err := client.WriteJSON(msg)
			if err != nil {
				client.Close()
				delete(clients, client)
			}
		}
	}
}

func main() {
	// 加载端口配置
	loadConfig()
	// 加载历史聊天记录
	loadHistory()
	go broadcastLoop()

	http.HandleFunc("/", indexPage)
	http.HandleFunc("/rest", restPage)
	http.HandleFunc("/rest/log", logClearHandle)
	http.HandleFunc("/ws", wsConnectHandle)

	addr := ":" + cfg.ListenPort
	log.Println("聊天室已启动")
	log.Printf("监听地址：http://127.0.0.1%s", addr)
	log.Println("清除本地登录页面：/rest")
	log.Fatal(http.ListenAndServe(addr, nil))
}
