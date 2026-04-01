package main

import (
	"html/template"
	"net/http"
	"os"
	"strconv"

	cshAuth "github.com/computersciencehouse/csh-auth"
	"github.com/computersciencehouse/vote/database"
	"github.com/computersciencehouse/vote/logging"
	"github.com/computersciencehouse/vote/sse"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"mvdan.cc/xurls/v2"
)

var VOTE_TOKEN = os.Getenv("VOTE_TOKEN")
var CONDITIONAL_GATEKEEP_URL = os.Getenv("VOTE_CONDITIONAL_URL")
var VOTE_HOST = os.Getenv("VOTE_HOST")

// Dev mode flags
var DEV_DISABLE_ACTIVE_FILTERS bool = os.Getenv("DEV_DISABLE_ACTIVE_FILTERS") == "true"
var DEV_FORCE_IS_EBOARD bool = os.Getenv("DEV_FORCE_IS_EBOARD") == "true"
var DEV_FORCE_IS_EVALS bool = os.Getenv("DEV_FORCE_IS_EVALS") == "true"

func inc(x int) string {
	return strconv.Itoa(x + 1)
}

func MakeLinks(s string) template.HTML {
	rx := xurls.Strict()
	s = template.HTMLEscapeString(s)
	safe := rx.ReplaceAllString(s, `<a href="$0" target="_blank">$0</a>`)
	return template.HTML(safe)
}

var oidcClient = OIDCClient{}
var broker *sse.Broker

func main() {
	godotenv.Load()
	database.Client = database.Connect()
	r := gin.Default()
	r.StaticFS("/static", http.Dir("static"))
	r.SetFuncMap(template.FuncMap{
		"inc":       inc,
		"MakeLinks": MakeLinks,
	})
	r.LoadHTMLGlob("templates/*")
	broker = sse.NewBroker()

	csh := cshAuth.CSHAuth{}
	csh.Init(
		os.Getenv("VOTE_OIDC_ID"),
		os.Getenv("VOTE_OIDC_SECRET"),
		os.Getenv("VOTE_JWT_SECRET"),
		os.Getenv("VOTE_STATE"),
		VOTE_HOST,
		VOTE_HOST+"/auth/callback",
		VOTE_HOST+"/auth/login",
		[]string{"profile", "email", "groups"},
	)
	oidcClient.setupOidcClient(os.Getenv("VOTE_OIDC_ID"), os.Getenv("VOTE_OIDC_SECRET"))
	InitConstitution()

	if DEV_DISABLE_ACTIVE_FILTERS {
		logging.Logger.WithFields(logrus.Fields{"method": "main init"}).Warning("Dev disable active filters is set!")
	}
	if DEV_FORCE_IS_EBOARD {
		logging.Logger.WithFields(logrus.Fields{"method": "main init"}).Warning("Dev force eboard is set!")
	}
	if DEV_FORCE_IS_EVALS {
		logging.Logger.WithFields(logrus.Fields{"method": "main init"}).Warning("Dev force evals is set!")
	}

	r.GET("/auth/login", csh.AuthRequest)
	r.GET("/auth/callback", csh.AuthCallback)
	r.GET("/auth/logout", csh.AuthLogout)

	r.GET("/", csh.AuthWrapper(GetHomepage))
	r.GET("/closed", csh.AuthWrapper(GetClosedPolls))

	r.GET("/create", csh.AuthWrapper(GetCreatePage))
	r.POST("/create", csh.AuthWrapper(CreatePoll))

	r.GET("/poll/:id", csh.AuthWrapper(GetPollById))
	r.POST("/poll/:id", csh.AuthWrapper(VoteInPoll))

	r.GET("/results/:id", csh.AuthWrapper(GetPollResults))

	r.POST("/poll/:id/hide", csh.AuthWrapper(HidePollResults))
	r.POST("/poll/:id/close", csh.AuthWrapper(ClosePoll))

	r.GET("/eboard", csh.AuthWrapper(HandleGetEboardVote))
	r.POST("/eboard", csh.AuthWrapper(HandlePostEboardVote))
	r.POST("/eboard/manage", csh.AuthWrapper(HandleManageEboardVote))

	r.GET("/stream/:topic", csh.AuthWrapper(broker.ServeHTTP))

	go broker.Listen()

	r.Run()
}
