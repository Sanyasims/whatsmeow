package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow/webtest/properties"
	"go.mau.fi/whatsmeow/webtest/webhook"
	"go.mau.fi/whatsmeow/webtest/ws"
	"io"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	waBinary "go.mau.fi/whatsmeow/binary"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

var cli *whatsmeow.Client
var log waLog.Logger
var logLevel = "INFO"
var debugLogs = flag.Bool("debug", true, "Enable debug logs?")
var dbDialect = flag.String("db-dialect", "sqlite3", "Database dialect (sqlite3 or postgres)")
var dbAddress = flag.String("db-address", "file:webtest.db?_foreign_keys=on", "Database address")
var requestFullSync = flag.Bool("request-full-sync", false, "Request full (1 year) history sync when logging in?")
var pairRejectChan = make(chan bool, 1)
var historySyncID int32
var startupTime = time.Now().Unix()
var config properties.Configuration

// Стартовый метод
func main() {

	waBinary.IndentXML = true

	flag.Parse()

	if *debugLogs {
		logLevel = "DEBUG"
	}

	if *requestFullSync {
		store.DeviceProps.RequireFullSync = proto.Bool(true)
	}
	log = waLog.Stdout("Main", logLevel, true)

	log.Infof("Run app")

	// считываем файл кофигурации
	content, err := os.ReadFile("config.json")

	// если есть ошибка
	if err != nil {

		fmt.Println("Error when opening config file")

		//логируем ошибку
		log.Errorf("Error when opening config file: ", err)

		//не продолжаем
		return
	}

	// лесериализуем из JSON
	err = json.Unmarshal(content, &config)

	// если есть ошибка
	if err != nil {

		//логируем ошибку
		log.Errorf("Error during parse Configuration: ", err)

		//не продолжаем
		return
	}

	//TODO:получать порт из БД

	//создаем экземпляр Engine
	engine := gin.Default()

	//websocket
	engine.GET("/ws", wsHandle)

	//маршрутизация запуска инстанса
	engine.GET("/runInstance", runInstance)

	//маршрутизация отправки сообщения
	engine.POST("/sendMessage", sendMessage)

	//запускаем сервер
	err = engine.Run(config.Host)

	//если есть ошибка
	if err != nil {

		//выводим лог
		log.Errorf("Failed to start server: %v", err)

		//не продолжаем
		return
	}
}

// Метод парсит идентификатор Whatsapp
func parseJID(arg string) (types.JID, bool) {
	if arg[0] == '+' {
		arg = arg[1:]
	}
	if !strings.ContainsRune(arg, '@') {
		return types.NewJID(arg, types.DefaultUserServer), true
	} else {
		recipient, err := types.ParseJID(arg)
		if err != nil {
			log.Errorf("Invalid JID %s: %v", arg, err)
			return recipient, false
		} else if recipient.User == "" {
			log.Errorf("Invalid JID %s: no server specified", arg)
			return recipient, false
		}
		return recipient, true
	}
}

// Метод обрабатывает команду
func handleCmd(cmd string, args []string) {
	switch cmd {
	case "reconnect":
		cli.Disconnect()
		err := cli.Connect()
		if err != nil {
			log.Errorf("Failed to connect: %v", err)
		}
	case "logout":
		err := cli.Logout()
		if err != nil {
			log.Errorf("Error logging out: %v", err)
		} else {
			log.Infof("Successfully logged out")
		}
	case "appstate":
		if len(args) < 1 {
			log.Errorf("Usage: appstate <types...>")
			return
		}
		names := []appstate.WAPatchName{appstate.WAPatchName(args[0])}
		if args[0] == "all" {
			names = []appstate.WAPatchName{appstate.WAPatchRegular, appstate.WAPatchRegularHigh, appstate.WAPatchRegularLow, appstate.WAPatchCriticalUnblockLow, appstate.WAPatchCriticalBlock}
		}
		resync := len(args) > 1 && args[1] == "resync"
		for _, name := range names {
			err := cli.FetchAppState(name, resync, false)
			if err != nil {
				log.Errorf("Failed to sync app state: %v", err)
			}
		}
	case "request-appstate-key":
		if len(args) < 1 {
			log.Errorf("Usage: request-appstate-key <ids...>")
			return
		}
		var keyIDs = make([][]byte, len(args))
		for i, id := range args {
			decoded, err := hex.DecodeString(id)
			if err != nil {
				log.Errorf("Failed to decode %s as hex: %v", id, err)
				return
			}
			keyIDs[i] = decoded
		}
		cli.DangerousInternals().RequestAppStateKeys(context.Background(), keyIDs)
	case "checkuser":
		if len(args) < 1 {
			log.Errorf("Usage: checkuser <phone numbers...>")
			return
		}
		resp, err := cli.IsOnWhatsApp(args)
		if err != nil {
			log.Errorf("Failed to check if users are on WhatsApp:", err)
		} else {
			for _, item := range resp {
				if item.VerifiedName != nil {
					log.Infof("%s: on whatsapp: %t, JID: %s, business name: %s", item.Query, item.IsIn, item.JID, item.VerifiedName.Details.GetVerifiedName())
				} else {
					log.Infof("%s: on whatsapp: %t, JID: %s", item.Query, item.IsIn, item.JID)
				}
			}
		}
	case "checkupdate":
		resp, err := cli.CheckUpdate()
		if err != nil {
			log.Errorf("Failed to check for updates: %v", err)
		} else {
			log.Debugf("Version data: %#v", resp)
			if resp.ParsedVersion == store.GetWAVersion() {
				log.Infof("Client is up to date")
			} else if store.GetWAVersion().LessThan(resp.ParsedVersion) {
				log.Warnf("Client is outdated")
			} else {
				log.Infof("Client is newer than latest")
			}
		}
	case "subscribepresence":
		if len(args) < 1 {
			log.Errorf("Usage: subscribepresence <jid>")
			return
		}
		jid, ok := parseJID(args[0])
		if !ok {
			return
		}
		err := cli.SubscribePresence(jid)
		if err != nil {
			fmt.Println(err)
		}
	case "presence":
		if len(args) == 0 {
			log.Errorf("Usage: presence <available/unavailable>")
			return
		}
		fmt.Println(cli.SendPresence(types.Presence(args[0])))
	case "chatpresence":
		if len(args) == 2 {
			args = append(args, "")
		} else if len(args) < 2 {
			log.Errorf("Usage: chatpresence <jid> <composing/paused> [audio]")
			return
		}
		jid, _ := types.ParseJID(args[0])
		fmt.Println(cli.SendChatPresence(jid, types.ChatPresence(args[1]), types.ChatPresenceMedia(args[2])))
	case "privacysettings":
		resp, err := cli.TryFetchPrivacySettings(false)
		if err != nil {
			fmt.Println(err)
		} else {
			fmt.Printf("%+v\n", resp)
		}
	case "getuser":
		if len(args) < 1 {
			log.Errorf("Usage: getuser <jids...>")
			return
		}
		var jids []types.JID
		for _, arg := range args {
			jid, ok := parseJID(arg)
			if !ok {
				return
			}
			jids = append(jids, jid)
		}
		resp, err := cli.GetUserInfo(jids)
		if err != nil {
			log.Errorf("Failed to get user info: %v", err)
		} else {
			for jid, info := range resp {
				log.Infof("%s: %+v", jid, info)
			}
		}
	case "mediaconn":
		conn, err := cli.DangerousInternals().RefreshMediaConn(false)
		if err != nil {
			log.Errorf("Failed to get media connection: %v", err)
		} else {
			log.Infof("Media connection: %+v", conn)
		}
	case "getavatar":
		if len(args) < 1 {
			log.Errorf("Usage: getavatar <jid> [existing ID] [--preview] [--community]")
			return
		}
		jid, ok := parseJID(args[0])
		if !ok {
			return
		}
		existingID := ""
		if len(args) > 2 {
			existingID = args[2]
		}
		var preview, isCommunity bool
		for _, arg := range args {
			if arg == "--preview" {
				preview = true
			} else if arg == "--community" {
				isCommunity = true
			}
		}
		pic, err := cli.GetProfilePictureInfo(jid, &whatsmeow.GetProfilePictureParams{
			Preview:     preview,
			IsCommunity: isCommunity,
			ExistingID:  existingID,
		})
		if err != nil {
			log.Errorf("Failed to get avatar: %v", err)
		} else if pic != nil {
			log.Infof("Got avatar ID %s: %s", pic.ID, pic.URL)
		} else {
			log.Infof("No avatar found")
		}
	case "getgroup":
		if len(args) < 1 {
			log.Errorf("Usage: getgroup <jid>")
			return
		}
		group, ok := parseJID(args[0])
		if !ok {
			return
		} else if group.Server != types.GroupServer {
			log.Errorf("Input must be a group JID (@%s)", types.GroupServer)
			return
		}
		resp, err := cli.GetGroupInfo(group)
		if err != nil {
			log.Errorf("Failed to get group info: %v", err)
		} else {
			log.Infof("Group info: %+v", resp)
		}
	case "subgroups":
		if len(args) < 1 {
			log.Errorf("Usage: subgroups <jid>")
			return
		}
		group, ok := parseJID(args[0])
		if !ok {
			return
		} else if group.Server != types.GroupServer {
			log.Errorf("Input must be a group JID (@%s)", types.GroupServer)
			return
		}
		resp, err := cli.GetSubGroups(group)
		if err != nil {
			log.Errorf("Failed to get subgroups: %v", err)
		} else {
			for _, sub := range resp {
				log.Infof("Subgroup: %+v", sub)
			}
		}
	case "communityparticipants":
		if len(args) < 1 {
			log.Errorf("Usage: communityparticipants <jid>")
			return
		}
		group, ok := parseJID(args[0])
		if !ok {
			return
		} else if group.Server != types.GroupServer {
			log.Errorf("Input must be a group JID (@%s)", types.GroupServer)
			return
		}
		resp, err := cli.GetLinkedGroupsParticipants(group)
		if err != nil {
			log.Errorf("Failed to get community participants: %v", err)
		} else {
			log.Infof("Community participants: %+v", resp)
		}
	case "listgroups":
		groups, err := cli.GetJoinedGroups()
		if err != nil {
			log.Errorf("Failed to get group list: %v", err)
		} else {
			for _, group := range groups {
				log.Infof("%+v", group)
			}
		}
	case "getinvitelink":
		if len(args) < 1 {
			log.Errorf("Usage: getinvitelink <jid> [--reset]")
			return
		}
		group, ok := parseJID(args[0])
		if !ok {
			return
		} else if group.Server != types.GroupServer {
			log.Errorf("Input must be a group JID (@%s)", types.GroupServer)
			return
		}
		resp, err := cli.GetGroupInviteLink(group, len(args) > 1 && args[1] == "--reset")
		if err != nil {
			log.Errorf("Failed to get group invite link: %v", err)
		} else {
			log.Infof("Group invite link: %s", resp)
		}
	case "queryinvitelink":
		if len(args) < 1 {
			log.Errorf("Usage: queryinvitelink <link>")
			return
		}
		resp, err := cli.GetGroupInfoFromLink(args[0])
		if err != nil {
			log.Errorf("Failed to resolve group invite link: %v", err)
		} else {
			log.Infof("Group info: %+v", resp)
		}
	case "querybusinesslink":
		if len(args) < 1 {
			log.Errorf("Usage: querybusinesslink <link>")
			return
		}
		resp, err := cli.ResolveBusinessMessageLink(args[0])
		if err != nil {
			log.Errorf("Failed to resolve business message link: %v", err)
		} else {
			log.Infof("Business info: %+v", resp)
		}
	case "joininvitelink":
		if len(args) < 1 {
			log.Errorf("Usage: acceptinvitelink <link>")
			return
		}
		groupID, err := cli.JoinGroupWithLink(args[0])
		if err != nil {
			log.Errorf("Failed to join group via invite link: %v", err)
		} else {
			log.Infof("Joined %s", groupID)
		}
	case "getstatusprivacy":
		resp, err := cli.GetStatusPrivacy()
		fmt.Println(err)
		fmt.Println(resp)
	case "setdisappeartimer":
		if len(args) < 2 {
			log.Errorf("Usage: setdisappeartimer <jid> <days>")
			return
		}
		days, err := strconv.Atoi(args[1])
		if err != nil {
			log.Errorf("Invalid duration: %v", err)
			return
		}
		recipient, ok := parseJID(args[0])
		if !ok {
			return
		}
		err = cli.SetDisappearingTimer(recipient, time.Duration(days)*24*time.Hour)
		if err != nil {
			log.Errorf("Failed to set disappearing timer: %v", err)
		}
	case "send":
		if len(args) < 2 {
			log.Errorf("Usage: send <jid> <text>")
			return
		}
		recipient, ok := parseJID(args[0])
		if !ok {
			return
		}
		msg := &waProto.Message{Conversation: proto.String(strings.Join(args[1:], " "))}
		resp, err := cli.SendMessage(context.Background(), recipient, msg)
		if err != nil {
			log.Errorf("Error sending message: %v", err)
		} else {
			log.Infof("Message sent (server timestamp: %s)", resp.Timestamp)
		}
	case "sendpoll":
		if len(args) < 7 {
			log.Errorf("Usage: sendpoll <jid> <max answers> <question> -- <option 1> / <option 2> / ...")
			return
		}
		recipient, ok := parseJID(args[0])
		if !ok {
			return
		}
		maxAnswers, err := strconv.Atoi(args[1])
		if err != nil {
			log.Errorf("Number of max answers must be an integer")
			return
		}
		remainingArgs := strings.Join(args[2:], " ")
		question, optionsStr, _ := strings.Cut(remainingArgs, "--")
		question = strings.TrimSpace(question)
		options := strings.Split(optionsStr, "/")
		for i, opt := range options {
			options[i] = strings.TrimSpace(opt)
		}
		resp, err := cli.SendMessage(context.Background(), recipient, cli.BuildPollCreation(question, options, maxAnswers))
		if err != nil {
			log.Errorf("Error sending message: %v", err)
		} else {
			log.Infof("Message sent (server timestamp: %s)", resp.Timestamp)
		}
	case "multisend":
		if len(args) < 3 {
			log.Errorf("Usage: multisend <jids...> -- <text>")
			return
		}
		var recipients []types.JID
		for len(args) > 0 && args[0] != "--" {
			recipient, ok := parseJID(args[0])
			args = args[1:]
			if !ok {
				return
			}
			recipients = append(recipients, recipient)
		}
		if len(args) == 0 {
			log.Errorf("Usage: multisend <jids...> -- <text> (the -- is required)")
			return
		}
		msg := &waProto.Message{Conversation: proto.String(strings.Join(args[1:], " "))}
		for _, recipient := range recipients {
			go func(recipient types.JID) {
				resp, err := cli.SendMessage(context.Background(), recipient, msg)
				if err != nil {
					log.Errorf("Error sending message to %s: %v", recipient, err)
				} else {
					log.Infof("Message sent to %s (server timestamp: %s)", recipient, resp.Timestamp)
				}
			}(recipient)
		}
	case "react":
		if len(args) < 3 {
			log.Errorf("Usage: react <jid> <message ID> <reaction>")
			return
		}
		recipient, ok := parseJID(args[0])
		if !ok {
			return
		}
		messageID := args[1]
		fromMe := false
		if strings.HasPrefix(messageID, "me:") {
			fromMe = true
			messageID = messageID[len("me:"):]
		}
		reaction := args[2]
		if reaction == "remove" {
			reaction = ""
		}
		msg := &waProto.Message{
			ReactionMessage: &waProto.ReactionMessage{
				Key: &waProto.MessageKey{
					RemoteJid: proto.String(recipient.String()),
					FromMe:    proto.Bool(fromMe),
					Id:        proto.String(messageID),
				},
				Text:              proto.String(reaction),
				SenderTimestampMs: proto.Int64(time.Now().UnixMilli()),
			},
		}
		resp, err := cli.SendMessage(context.Background(), recipient, msg)
		if err != nil {
			log.Errorf("Error sending reaction: %v", err)
		} else {
			log.Infof("Reaction sent (server timestamp: %s)", resp.Timestamp)
		}
	case "revoke":
		if len(args) < 2 {
			log.Errorf("Usage: revoke <jid> <message ID>")
			return
		}
		recipient, ok := parseJID(args[0])
		if !ok {
			return
		}
		messageID := args[1]
		resp, err := cli.SendMessage(context.Background(), recipient, cli.BuildRevoke(recipient, types.EmptyJID, messageID))
		if err != nil {
			log.Errorf("Error sending revocation: %v", err)
		} else {
			log.Infof("Revocation sent (server timestamp: %s)", resp.Timestamp)
		}
	case "sendimg":
		if len(args) < 2 {
			log.Errorf("Usage: sendimg <jid> <image path> [caption]")
			return
		}
		recipient, ok := parseJID(args[0])
		if !ok {
			return
		}
		data, err := os.ReadFile(args[1])
		if err != nil {
			log.Errorf("Failed to read %s: %v", args[0], err)
			return
		}
		uploaded, err := cli.Upload(context.Background(), data, whatsmeow.MediaImage)
		if err != nil {
			log.Errorf("Failed to upload file: %v", err)
			return
		}
		msg := &waProto.Message{ImageMessage: &waProto.ImageMessage{
			Caption:       proto.String(strings.Join(args[2:], " ")),
			Url:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(http.DetectContentType(data)),
			FileEncSha256: uploaded.FileEncSHA256,
			FileSha256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(data))),
		}}
		resp, err := cli.SendMessage(context.Background(), recipient, msg)
		if err != nil {
			log.Errorf("Error sending image message: %v", err)
		} else {
			log.Infof("Image message sent (server timestamp: %s)", resp.Timestamp)
		}
	case "setstatus":
		if len(args) == 0 {
			log.Errorf("Usage: setstatus <message>")
			return
		}
		err := cli.SetStatusMessage(strings.Join(args, " "))
		if err != nil {
			log.Errorf("Error setting status message: %v", err)
		} else {
			log.Infof("Status updated")
		}
	case "archive":
		if len(args) < 2 {
			log.Errorf("Usage: archive <jid> <action>")
			return
		}
		target, ok := parseJID(args[0])
		if !ok {
			return
		}
		action, err := strconv.ParseBool(args[1])
		if err != nil {
			log.Errorf("invalid second argument: %v", err)
			return
		}

		err = cli.SendAppState(appstate.BuildArchive(target, action, time.Time{}, nil))
		if err != nil {
			log.Errorf("Error changing chat's archive state: %v", err)
		}
	case "mute":
		if len(args) < 2 {
			log.Errorf("Usage: mute <jid> <action>")
			return
		}
		target, ok := parseJID(args[0])
		if !ok {
			return
		}
		action, err := strconv.ParseBool(args[1])
		if err != nil {
			log.Errorf("invalid second argument: %v", err)
			return
		}

		err = cli.SendAppState(appstate.BuildMute(target, action, 1*time.Hour))
		if err != nil {
			log.Errorf("Error changing chat's mute state: %v", err)
		}
	case "pin":
		if len(args) < 2 {
			log.Errorf("Usage: pin <jid> <action>")
			return
		}
		target, ok := parseJID(args[0])
		if !ok {
			return
		}
		action, err := strconv.ParseBool(args[1])
		if err != nil {
			log.Errorf("invalid second argument: %v", err)
			return
		}

		err = cli.SendAppState(appstate.BuildPin(target, action))
		if err != nil {
			log.Errorf("Error changing chat's pin state: %v", err)
		}
	}
}

// Метод обрабатывает callback
func handler(rawEvt interface{}) {
	switch evt := rawEvt.(type) {
	case *events.AppStateSyncComplete:
		if len(cli.Store.PushName) > 0 && evt.Name == appstate.WAPatchCriticalBlock {
			err := cli.SendPresence(types.PresenceAvailable)
			if err != nil {
				log.Warnf("Failed to send available presence: %v", err)
			} else {
				log.Infof("Marked self as available")
			}
		}
	case *events.Connected, *events.PushNameSetting:
		if len(cli.Store.PushName) == 0 {
			return
		}
		// Send presence available when connecting and when the pushname is changed.
		// This makes sure that outgoing messages always have the right pushname.
		err := cli.SendPresence(types.PresenceAvailable)
		if err != nil {
			log.Warnf("Failed to send available presence: %v", err)
		} else {
			log.Infof("Marked self as available")
		}
	case *events.StreamReplaced:
		os.Exit(0)
	case *events.Message:
		metaParts := []string{fmt.Sprintf("pushname: %s", evt.Info.PushName), fmt.Sprintf("timestamp: %s", evt.Info.Timestamp)}
		if evt.Info.Type != "" {
			metaParts = append(metaParts, fmt.Sprintf("type: %s", evt.Info.Type))
		}
		if evt.Info.Category != "" {
			metaParts = append(metaParts, fmt.Sprintf("category: %s", evt.Info.Category))
		}
		if evt.IsViewOnce {
			metaParts = append(metaParts, "view once")
		}
		if evt.IsViewOnce {
			metaParts = append(metaParts, "ephemeral")
		}
		if evt.IsViewOnceV2 {
			metaParts = append(metaParts, "ephemeral (v2)")
		}
		if evt.IsDocumentWithCaption {
			metaParts = append(metaParts, "document with caption")
		}
		if evt.IsEdit {
			metaParts = append(metaParts, "edit")
		}

		log.Infof("Received message %s from %s (%s): %+v", evt.Info.ID, evt.Info.SourceString(), strings.Join(metaParts, ", "), evt.Message)

		// если текстовое сообщение
		if evt.Info.Type == "text" {

			// создаем объект данных webhook о новом сообщении
			newMessageWebhook := webhook.NewMessageWebhook{
				TypeWebhook:     "newMessage",
				WebhookUrl:      config.WebhookUrl,
				CountTrySending: 0,
				InstanceWhatsapp: webhook.InstanceWhatsappWebhook{
					IdInstance: 0,
					Wid:        cli.Store.ID.User + "@c.us",
				},
				Timestamp: time.Now().Unix(),
				NewMessage: webhook.NewMessage{
					Chat: webhook.ChatDataWhatsappMessage{
						ChatId:    evt.Info.Chat.User + "@c.us",
						FromMe:    evt.Info.IsFromMe,
						IdMessage: evt.Info.ID,
					},
					Message: webhook.DataWhatsappMessage{
						TypeMessage: "textMessage",
						Text:        *evt.Message.Conversation,
					},
					MessageTimestamp: evt.Info.Timestamp.Unix(),
					Status:           "delivered",
				},
			}

			// отправляем вебхук
			webhook.SendNewMessageWebhook(newMessageWebhook, log)
		}

		if evt.Message.GetPollUpdateMessage() != nil {
			decrypted, err := cli.DecryptPollVote(evt)
			if err != nil {
				log.Errorf("Failed to decrypt vote: %v", err)
			} else {
				log.Infof("Selected options in decrypted vote:")
				for _, option := range decrypted.SelectedOptions {
					log.Infof("- %X", option)
				}
			}
		} else if evt.Message.GetEncReactionMessage() != nil {
			decrypted, err := cli.DecryptReaction(evt)
			if err != nil {
				log.Errorf("Failed to decrypt encrypted reaction: %v", err)
			} else {
				log.Infof("Decrypted reaction: %+v", decrypted)
			}
		}

		img := evt.Message.GetImageMessage()
		if img != nil {
			data, err := cli.Download(img)
			if err != nil {
				log.Errorf("Failed to download image: %v", err)
				return
			}
			exts, _ := mime.ExtensionsByType(img.GetMimetype())
			path := fmt.Sprintf("%s%s", evt.Info.ID, exts[0])
			err = os.WriteFile(path, data, 0600)
			if err != nil {
				log.Errorf("Failed to save image: %v", err)
				return
			}
			log.Infof("Saved image in message to %s", path)
		}
	case *events.Receipt:
		if evt.Type == events.ReceiptTypeRead || evt.Type == events.ReceiptTypeReadSelf {

			// выводим лог
			log.Infof("%v was read by %s at %s", evt.MessageIDs, evt.SourceString(), evt.Timestamp)

			// обходим массив идентифкаторов сообщений
			for _, idMessage := range evt.MessageIDs {

				//создаем структуру вебхук о статусе сообщения
				statusMessageWebhook := webhook.StatusMessageWebhook{
					TypeWebhook:     "statusMessage",
					WebhookUrl:      config.WebhookUrl,
					CountTrySending: 0,
					InstanceWhatsapp: webhook.InstanceWhatsappWebhook{
						IdInstance: 0,
						Wid:        cli.Store.ID.User + "@c.us",
					},
					Timestamp: time.Now().Unix(),
					StatusMessage: webhook.DataStatusMessage{
						IdMessage:       idMessage,
						TimestampStatus: evt.Timestamp.Unix(),
						Status:          "read",
					},
				}

				//отправляем вебхук
				webhook.SendStatusMessageWebhook(statusMessageWebhook, log)
			}

		} else if evt.Type == events.ReceiptTypeDelivered {

			//выводим лог
			log.Infof("%s was delivered to %s at %s", evt.MessageIDs[0], evt.SourceString(), evt.Timestamp)

			// обходим массив идентифкаторов сообщений
			for _, idMessage := range evt.MessageIDs {

				//создаем структуру вебхук о статусе сообщения
				statusMessageWebhook := webhook.StatusMessageWebhook{
					TypeWebhook:     "statusMessage",
					WebhookUrl:      config.WebhookUrl,
					CountTrySending: 0,
					InstanceWhatsapp: webhook.InstanceWhatsappWebhook{
						IdInstance: 0,
						Wid:        cli.Store.ID.User + "@c.us",
					},
					Timestamp: time.Now().Unix(),
					StatusMessage: webhook.DataStatusMessage{
						IdMessage:       idMessage,
						TimestampStatus: evt.Timestamp.Unix(),
						Status:          "delivered",
					},
				}

				//отправляем вебхук
				webhook.SendStatusMessageWebhook(statusMessageWebhook, log)
			}
		}
	case *events.Presence:
		if evt.Unavailable {
			if evt.LastSeen.IsZero() {
				log.Infof("%s is now offline", evt.From)
			} else {
				log.Infof("%s is now offline (last seen: %s)", evt.From, evt.LastSeen)
			}
		} else {
			log.Infof("%s is now online", evt.From)
		}
	case *events.HistorySync:
		id := atomic.AddInt32(&historySyncID, 1)
		fileName := fmt.Sprintf("history-%d-%d.json", startupTime, id)
		file, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			log.Errorf("Failed to open file to write history sync: %v", err)
			return
		}
		enc := json.NewEncoder(file)
		enc.SetIndent("", "  ")
		err = enc.Encode(evt.Data)
		if err != nil {
			log.Errorf("Failed to write history sync: %v", err)
			return
		}
		log.Infof("Wrote history sync to %s", fileName)
		_ = file.Close()
	case *events.AppState:
		log.Debugf("App state event: %+v / %+v", evt.Index, evt.SyncActionValue)
	case *events.KeepAliveTimeout:
		log.Debugf("Keepalive timeout event: %+v", evt)
		if evt.ErrorCount > 3 {
			log.Debugf("Got >3 keepalive timeouts, forcing reconnect")
			go func() {
				cli.Disconnect()
				err := cli.Connect()
				if err != nil {
					log.Errorf("Error force-reconnecting after keepalive timeouts: %v", err)
				}
			}()
		}
	case *events.KeepAliveRestored:
		log.Debugf("Keepalive restored")
	}
}

// Метод запускает инстанс
func runInstance(ctx *gin.Context) {

	// запускаем инстанс в отдельном потоке
	go startInstance()

	// отдаем ответ
	ctx.JSON(200, gin.H{
		"success": true,
	})

	time.Sleep(10 * time.Second)

	return
}

// Метод запускает инстанс
func startInstance() {

	dbLog := waLog.Stdout("Database", logLevel, true)

	storeContainer, err := sqlstore.New(*dbDialect, *dbAddress, dbLog)

	if err != nil {

		log.Errorf("Failed to connect to database: %v", err)

		return
	}

	device, err := storeContainer.GetFirstDevice()

	if err != nil {

		log.Errorf("Failed to get device: %v", err)

		return
	}

	cli = whatsmeow.NewClient(device, waLog.Stdout("Client", logLevel, true))

	var isWaitingForPair atomic.Bool

	cli.PrePairCallback = func(jid types.JID, platform, businessName string) bool {

		isWaitingForPair.Store(true)

		defer isWaitingForPair.Store(false)

		log.Infof("Pairing %s (platform: %q, business name: %q). Type r within 3 seconds to reject pair", jid, platform, businessName)

		select {
		case reject := <-pairRejectChan:

			if reject {

				log.Infof("Rejecting pair")

				return false
			}
		case <-time.After(3 * time.Second):
		}

		log.Infof("Accepting pair")

		return true
	}

	ch, err := cli.GetQRChannel(context.Background())

	if err != nil {

		// This error means that we're already logged in, so ignore it.
		if !errors.Is(err, whatsmeow.ErrQRStoreContainsID) {

			log.Errorf("Failed to get QR channel: %v", err)
		}
	} else {

		go func() {

			for evt := range ch {

				if evt.Event == "code" {

					qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)

				} else {

					log.Infof("QR channel result: %s", evt.Event)
				}
			}
		}()
	}

	cli.AddEventHandler(handler)

	// получаем прокси
	proxy, err := config.GetProxy()

	// если ошибка
	if err != nil {

		// логируем ошибку
		log.Errorf("Failed get proxy from config: %v", err)

		// не продолжаем
		return
	}

	// устанавливаем прокси
	cli.SetProxy(proxy)

	err = cli.Connect()

	if err != nil {

		log.Errorf("Failed to connect: %v", err)

		return
	}

	c := make(chan os.Signal)

	input := make(chan string)

	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {

		defer close(input)

		scan := bufio.NewScanner(os.Stdin)

		for scan.Scan() {

			line := strings.TrimSpace(scan.Text())

			if len(line) > 0 {

				input <- line
			}
		}
	}()

	for {
		select {
		case <-c:

			log.Infof("Interrupt received, exiting")

			cli.Disconnect()

			return
		case cmd := <-input:

			if len(cmd) == 0 {

				//log.Infof("Stdin closed, exiting")
				//
				//cli.Disconnect()

				return
			}
			if isWaitingForPair.Load() {

				if cmd == "r" {

					pairRejectChan <- true

				} else if cmd == "a" {

					pairRejectChan <- false
				}

				continue
			}

			args := strings.Fields(cmd)

			cmd = args[0]

			args = args[1:]

			go handleCmd(strings.ToLower(cmd), args)
		}
	}
}

// Метод отправляет сообщение
func sendMessage(ctx *gin.Context) {

	// считываем тело запроса
	content, err := io.ReadAll(ctx.Request.Body)

	// если есть ошибка
	if err != nil {

		//логируем ошибку
		log.Errorf("Error read body request: ", err)

		ctx.JSON(400, gin.H{
			"reason": "Bad request data",
		})

		//не продолжаем
		return
	}

	// объявляем структуру отправки текстового сообщения
	var requestSendMessage properties.RequestSendMessage

	// лесериализуем из JSON
	err = json.Unmarshal(content, &requestSendMessage)

	// если есть ошибка
	if err != nil {

		//логируем ошибку
		log.Errorf("Error during parse RequestSendMessage: ", err)

		ctx.JSON(400, gin.H{
			"reason": "Bad request data",
		})

		//не продолжаем
		return
	}

	//TODO проверять валидность данных

	// парсим идентифкатор Whatsapp, если chatId то его
	recipient, ok := parseJID(strconv.FormatInt(requestSendMessage.Phone, 10))

	if !ok {

		ctx.JSON(400, gin.H{
			"reason": "Bad request data",
		})

		//отдаем
		return
	}

	msg := &waProto.Message{Conversation: proto.String(requestSendMessage.Message)}

	resp, err := cli.SendMessage(context.Background(), recipient, msg)

	if err != nil {

		log.Errorf("Error sending message: %v", err)

		ctx.JSON(500, gin.H{
			"reason": "Error sending message",
		})

	} else {

		log.Infof("Message sent (server timestamp: %s)", resp.Timestamp)

		ctx.JSON(200, gin.H{
			"id": resp.ID,
		})

		//создаем структуру вебхук о статусе сообщения
		statusMessageWebhook := webhook.StatusMessageWebhook{
			TypeWebhook:     "statusMessage",
			WebhookUrl:      config.WebhookUrl,
			CountTrySending: 0,
			InstanceWhatsapp: webhook.InstanceWhatsappWebhook{
				IdInstance: 0,
				Wid:        cli.Store.ID.User + "@c.us",
			},
			Timestamp: time.Now().Unix(),
			StatusMessage: webhook.DataStatusMessage{
				IdMessage:       resp.ID,
				TimestampStatus: resp.Timestamp.Unix(),
				Status:          "sent",
			},
		}

		//отправляем вебхук
		webhook.SendStatusMessageWebhook(statusMessageWebhook, log)
	}
}

// Метод обрабатывет сокет соединение
func wsHandle(ctx *gin.Context) {

	//Upgrade the HTTP protocol to the websocket protocol
	conn, err := ws.Upgrader.Upgrade(ctx.Writer, ctx.Request, nil)

	// проверяем ошибку
	if err != nil {

		// отдаем ошибку
		http.NotFound(ctx.Writer, ctx.Request)

		// не продолжаем
		return
	}

	// сохдаем клиент ws
	client := &ws.Client{
		Socket: conn,
	}

	//Start the message to collect the news from the web side
	go client.Read() //статичный метод
}
