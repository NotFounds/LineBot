package main

import (
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"google.golang.org/appengine"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/taskqueue"
	"google.golang.org/appengine/urlfetch"

	"golang.org/x/net/context"

	"github.com/joho/godotenv"
	"github.com/line/line-bot-sdk-go/linebot"
	"github.com/line/line-bot-sdk-go/linebot/httphandler"
)

var botHandler *httphandler.WebhookHandler

func init() {
	err := godotenv.Load("line.env")
	if err != nil {
		panic(err)
	}
	botHandler, err = httphandler.New(
		os.Getenv("LINE_BOT_CHANNEL_SECRET"),
		os.Getenv("LINE_BOT_CHANNEL_TOKEN"),
	)
	botHandler.HandleEvents(handleCallback)

	http.Handle("/callback", botHandler)
	http.HandleFunc("/task", handleTask)
}

func newLINEBot(c context.Context) (*linebot.Client, error) {
	return botHandler.NewClient(
		linebot.WithHTTPClient(urlfetch.Client(c)),
	)
}

func newContext(r *http.Request) context.Context {
	return appengine.NewContext(r)
}

func logf(c context.Context, format string, args ...interface{}) {
	log.Infof(c, format, args...)
}

func errf(c context.Context, format string, args ...interface{}) {
	log.Errorf(c, format, args...)
}

func handleCallback(evs []*linebot.Event, r *http.Request) {
	c := newContext(r)
	tasks := make([]*taskqueue.Task, len(evs))
	for i, e := range evs {
		j, err := json.Marshal(e)
		if err != nil {
			errf(c, "json.Marshal: %v", err)
			return
		}
		data := base64.StdEncoding.EncodeToString(j)
		tasks[i] = taskqueue.NewPOSTTask("/task", url.Values{"data": {data}})
	}
	taskqueue.AddMulti(c, tasks, "")
}

func getSearchUrl(q string) string {
	return "http://www.google.co.jp/search?hl=ja&source=hp&q=" + url.QueryEscape(q)
}

func getImgSearchUrl(q string) string {
	return getSearchUrl(q) + "&tbm=isch&tbs=ift:jpg"
}

func getMovSearchUrl(q string) string {
	return getSearchUrl(q) + "&tbm=vid"
}

func getNewsSearchUrl(q string) string {
	return getSearchUrl(q) + "&tbm=nws"
}

func getWikiUrl(q string) string {
	return "https://ja.wikipedia.org/wiki/" + q
}

func getOneImgUrl(q string, c context.Context) string {
	url := getImgSearchUrl(q)
	client := urlfetch.Client(c)
	resp, err := client.Get(url)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	s := string(buf)

	r := regexp.MustCompile(`<img.+?src=\"(.+?)\".+?>`)
	ret := r.FindStringSubmatch(s)
	if len(ret) == 2 {
		return ret[1]
	} else {
		return ""
	}
}

func handleTask(w http.ResponseWriter, r *http.Request) {
	c := newContext(r)
	data := r.FormValue("data")
	if data == "" {
		errf(c, "Data is Nothing")
		return
	}

	j, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		errf(c, "base64 DecodeString: %v", err)
		return
	}

	e := new(linebot.Event)
	err = json.Unmarshal(j, e)
	if err != nil {
		errf(c, "json.Unmarshal: %v", err)
		return
	}

	bot, err := newLINEBot(c)
	if err != nil {
		errf(c, "newLINEBot: %v", err)
		return
	}

	// out put log
	logf(c, "EventType: %s\nMessage: %#v\nEvent: %#v\n", e.Type, e.Message, e)

	if e.Type == linebot.EventTypeMessage {
		var obj linebot.Message
		switch msg := e.Message.(type) {
		case *linebot.TextMessage:
			text := strings.TrimSpace(msg.Text)
			slice := strings.Split(text, " ")
			idx := strings.Index(text, "とは?")

			if idx != -1 {
				obj = linebot.NewTextMessage(getSearchUrl(text[:idx]))
			} else if len(slice) >= 2 {
				switch slice[0] {
				case "img", "IMG", "I", "画像":
					url := getOneImgUrl(strings.Join(slice[1:], " "), c)
					if url != "" {
						obj = linebot.NewTextMessage(url)
					} else {
						obj = linebot.NewTextMessage(getImgSearchUrl(strings.Join(slice[1:], " ")))
					}
				case "mov", "MOV", "M", "動画":
					obj = linebot.NewTextMessage(getMovSearchUrl(strings.Join(slice[1:], " ")))
				case "search", "S", "ggr", "検索", "ググる":
					obj = linebot.NewTextMessage(getSearchUrl(strings.Join(slice[1:], " ")))
				case "news", "News", "ニュース":
					obj = linebot.NewTextMessage(getNewsSearchUrl(strings.Join(slice[1:], " ")))
				case "wikipedia", "wiki", "Wikipedia", "Wiki", "W", "ウィキペディア", "ウィキ":
					obj = linebot.NewTextMessage(getWikiUrl(slice[1]))
				}
			}
		}

		if obj != nil {
			if _, err = bot.ReplyMessage(e.ReplyToken, obj).WithContext(c).Do(); err != nil {
				errf(c, "ReplayMessage: %v", err)
				return
			}
		}
	}

	// set response code
	w.WriteHeader(200)
}
