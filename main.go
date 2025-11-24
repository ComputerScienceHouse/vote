package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	cshAuth "github.com/computersciencehouse/csh-auth"
	"github.com/computersciencehouse/vote/database"
	"github.com/computersciencehouse/vote/logging"
	"github.com/computersciencehouse/vote/sse"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"mvdan.cc/xurls/v2"
)

var VOTE_TOKEN = os.Getenv("VOTE_TOKEN")
var CONDITIONAL_GATEKEEP_URL = os.Getenv("VOTE_CONDITIONAL_URL")
var VOTE_HOST = os.Getenv("VOTE_HOST")

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

func main() {
	r := gin.Default()
	r.StaticFS("/static", http.Dir("static"))
	r.SetFuncMap(template.FuncMap{
		"inc":       inc,
		"MakeLinks": MakeLinks,
	})
	r.LoadHTMLGlob("templates/*")
	broker := sse.NewBroker()

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
	r.GET("/auth/login", csh.AuthRequest)
	r.GET("/auth/callback", csh.AuthCallback)
	r.GET("/auth/logout", csh.AuthLogout)

	// TODO: change ALL the response codes to use http.(actual description) 
	r.GET("/", csh.AuthWrapper(func(c *gin.Context) {
		cl, _ := c.Get("cshauth")
		claims := cl.(cshAuth.CSHClaims)
		// This is intentionally left unprotected
		// A user may be unable to vote but should still be able to see a list of polls

		polls, err := database.GetOpenPolls(c)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		sort.Slice(polls, func(i, j int) bool {
			return polls[i].Id > polls[j].Id
		})

		closedPolls, err := database.GetClosedVotedPolls(c, claims.UserInfo.Username)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		ownedPolls, err := database.GetClosedOwnedPolls(c, claims.UserInfo.Username)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		closedPolls = append(closedPolls, ownedPolls...)

		sort.Slice(closedPolls, func(i, j int) bool {
			return closedPolls[i].Id > closedPolls[j].Id
		})
		closedPolls = uniquePolls(closedPolls)

		c.HTML(200, "index.tmpl", gin.H{
			"Polls":       polls,
			"ClosedPolls": closedPolls,
			"Username":    claims.UserInfo.Username,
			"FullName":    claims.UserInfo.FullName,
		})
	}))

	r.GET("/create", csh.AuthWrapper(func(c *gin.Context) {
		cl, _ := c.Get("cshauth")
		claims := cl.(cshAuth.CSHClaims)
		if !slices.Contains(claims.UserInfo.Groups, "active") {
			c.HTML(403, "unauthorized.tmpl", gin.H{
				"Username": claims.UserInfo.Username,
				"FullName": claims.UserInfo.FullName,
			})
			return
		}

		c.HTML(200, "create.tmpl", gin.H{
			"Username": claims.UserInfo.Username,
			"FullName": claims.UserInfo.FullName,
			"IsEvals":  containsString(claims.UserInfo.Groups, "eboard-evaluations"),
		})
	}))

	r.POST("/create", csh.AuthWrapper(func(c *gin.Context) {
		cl, _ := c.Get("cshauth")
		claims := cl.(cshAuth.CSHClaims)
		if !slices.Contains(claims.UserInfo.Groups, "active") {
			c.HTML(403, "unauthorized.tmpl", gin.H{
				"Username": claims.UserInfo.Username,
				"FullName": claims.UserInfo.FullName,
			})
			return
		}

		quorumType := c.PostForm("quorumType")
		var quorum float64
		switch quorumType {
		case "12":
			quorum = 1.0 / 2.0
		case "23":
			quorum = 2.0 / 3.0
		default:
			quorum = 1.0 / 2.0
		}

		poll := &database.Poll{
			Id:               "",
			CreatedBy:        claims.UserInfo.Username,
			ShortDescription: c.PostForm("shortDescription"),
			LongDescription:  c.PostForm("longDescription"),
			VoteType:         database.POLL_TYPE_SIMPLE,
			OpenedTime:       time.Now(),
			Open:             true,
			QuorumType:       quorum,
			Hidden:           false,
			Gatekeep:         c.PostForm("gatekeep") == "true",
			AllowWriteIns:    c.PostForm("allowWriteIn") == "true",
		}
		if c.PostForm("rankedChoice") == "true" {
			poll.VoteType = database.POLL_TYPE_RANKED
		}

		switch c.PostForm("options") {
		case "pass-fail-conditional":
			poll.Options = []string{"Pass", "Fail/Conditional", "Abstain"}
		case "fail-conditional":
			poll.Options = []string{"Fail", "Conditional", "Abstain"}
		case "custom":
			poll.Options = []string{}
			for opt := range strings.SplitSeq(c.PostForm("customOptions"), ",") {
				poll.Options = append(poll.Options, strings.TrimSpace(opt))
				if !containsString(poll.Options, "Abstain") && (poll.VoteType == database.POLL_TYPE_SIMPLE) {
					poll.Options = append(poll.Options, "Abstain")
				}
			}
		case "pass-fail":
		default:
			poll.Options = []string{"Pass", "Fail", "Abstain"}
		}
		if poll.Gatekeep {
			if !slices.Contains(claims.UserInfo.Groups, "eboard-evaluations") {
				c.HTML(403, "unauthorized.tmpl", gin.H{
					"Username": claims.UserInfo.Username,
					"FullName": claims.UserInfo.FullName,
				})
				return
			}
			poll.AllowedUsers = GetEligibleVoters()
			for user := range strings.SplitSeq(c.PostForm("waivedUsers"), ",") {
				poll.AllowedUsers = append(poll.AllowedUsers, strings.TrimSpace(user))
			}
		}

		pollId, err := database.CreatePoll(c, poll)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.Redirect(302, "/poll/"+pollId)
	}))

	r.GET("/poll/:id", csh.AuthWrapper(func(c *gin.Context) {
		cl, _ := c.Get("cshauth")
		claims := cl.(cshAuth.CSHClaims)
		// This is intentionally left unprotected
		// We will check if a user can vote and redirect them to results if not later

		poll, err := database.GetPoll(c, c.Param("id"))
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		// If the user can't vote, just show them results
		if canVote(claims.UserInfo, *poll, poll.AllowedUsers) > 0 || !poll.Open {
			c.Redirect(302, "/results/"+poll.Id)
			return
		}

		writeInAdj := 0
		if poll.AllowWriteIns {
			writeInAdj = 1
		}

		canModify := containsString(claims.UserInfo.Groups, "active_rtp") || containsString(claims.UserInfo.Groups, "eboard") || poll.CreatedBy == claims.UserInfo.Username
		if poll.Gatekeep {
			canModify = false
		}

		c.HTML(200, "poll.tmpl", gin.H{
			"Id":               poll.Id,
			"ShortDescription": poll.ShortDescription,
			"LongDescription":  poll.LongDescription,
			"Options":          poll.Options,
			"PollType":         poll.VoteType,
			"RankedMax":        fmt.Sprint(len(poll.Options) + writeInAdj),
			"AllowWriteIns":    poll.AllowWriteIns,
			"CanModify":        canModify,
			"Username":         claims.UserInfo.Username,
			"FullName":         claims.UserInfo.FullName,
		})
	}))

	r.POST("/poll/:id", csh.AuthWrapper(func(c *gin.Context) {
		cl, _ := c.Get("cshauth")
		claims := cl.(cshAuth.CSHClaims)

		poll, err := database.GetPoll(c, c.Param("id"))
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		if canVote(claims.UserInfo, *poll, poll.AllowedUsers) > 0 || !poll.Open {
			c.Redirect(302, "/results/"+poll.Id)
			return
		}

		pId, err := primitive.ObjectIDFromHex(poll.Id)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		if poll.VoteType == database.POLL_TYPE_SIMPLE {
			vote := database.SimpleVote{
				Id:     "",
				PollId: pId,
				Option: c.PostForm("option"),
			}
			voter := database.Voter{
				PollId: pId,
				UserId: claims.UserInfo.Username,
			}

			if hasOption(poll, c.PostForm("option")) {
				vote.Option = c.PostForm("option")
			} else if poll.AllowWriteIns && c.PostForm("option") == "writein" {
				vote.Option = c.PostForm("writeinOption")
			} else {
				c.JSON(400, gin.H{"error": "Invalid Option"})
				return
			}
			database.CastSimpleVote(c, &vote, &voter)
		} else if poll.VoteType == database.POLL_TYPE_RANKED {
			vote := database.RankedVote{
				Id:      "",
				PollId:  pId,
				Options: make(map[string]int),
			}
			voter := database.Voter{
				PollId: pId,
				UserId: claims.UserInfo.Username,
			}

			fmt.Println(poll.Options)

			for _, option := range poll.Options {
				optionRankStr := c.PostForm(option)
				optionRank, err := strconv.Atoi(optionRankStr)

				if len(optionRankStr) < 1 {
					continue
				}
				if err != nil {
					c.JSON(400, gin.H{"error": "non-number ranking"})
					return
				}

				vote.Options[option] = optionRank
			}

			// process write-in
			if c.PostForm("writeinOption") != "" && c.PostForm("writein") != "" {
				for candidate := range vote.Options {
					if strings.EqualFold(candidate, strings.TrimSpace(c.PostForm("writeinOption"))) {
						c.JSON(500, gin.H{"error": "Write-in is already an option"})
						return
					}
				}
				rank, err := strconv.Atoi(c.PostForm("writein"))
				if err != nil {
					c.JSON(500, gin.H{"error": "Write-in rank is not numerical"})
					return
				}
				if rank < 1 {
					c.JSON(500, gin.H{"error": "Write-in rank is not positive"})
					return
				}
				vote.Options[c.PostForm("writeinOption")] = rank
			}

			maxNum := len(vote.Options)
			voted := make([]bool, maxNum)

			for _, rank := range vote.Options {
				if rank > 0 && rank <= maxNum {
					if voted[rank-1] {
						c.JSON(400, gin.H{"error": "You ranked two or more candidates at the same level"})
						return
					}
					voted[rank-1] = true
				} else {
					c.JSON(400, gin.H{"error": fmt.Sprintf("votes must be from 1 - %d", maxNum)})
					return
				}
			}

			rankedCandidates := len(vote.Options)
			for _, voteOpt := range vote.Options {
				if voteOpt > rankedCandidates {
					c.JSON(400, gin.H{"error": "Rank choice is more than the amount of candidates ranked"})
					return
				}
			}
			database.CastRankedVote(c, &vote, &voter)
		} else {
			c.JSON(500, gin.H{"error": "Unknown Poll Type"})
			return
		}

		if poll, err := database.GetPoll(c, c.Param("id")); err == nil {
			if results, err := poll.GetResult(c); err == nil {
				if bytes, err := json.Marshal(results); err == nil {
					broker.Notifier <- sse.NotificationEvent{
						EventName: poll.Id,
						Payload:   string(bytes),
					}
				}

			}
		}

		c.Redirect(302, "/results/"+poll.Id)
	}))

	r.GET("/results/:id", csh.AuthWrapper(func(c *gin.Context) {
		cl, _ := c.Get("cshauth")
		claims := cl.(cshAuth.CSHClaims)
		// This is intentionally left unprotected
		// A user may be unable to vote but still interested in the results of a poll

		poll, err := database.GetPoll(c, c.Param("id"))
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		if poll.Hidden && poll.CreatedBy != claims.UserInfo.Username {
			c.HTML(403, "hidden.tmpl", gin.H{
				"Username": claims.UserInfo.Username,
				"FullName": claims.UserInfo.FullName,
			})
			return
		}

		results, err := poll.GetResult(c)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		canModify := containsString(claims.UserInfo.Groups, "active_rtp") || containsString(claims.UserInfo.Groups, "eboard") || poll.CreatedBy == claims.UserInfo.Username
		if poll.Gatekeep {
			canModify = false
		}

		c.HTML(200, "result.tmpl", gin.H{
			"Id":               poll.Id,
			"ShortDescription": poll.ShortDescription,
			"LongDescription":  poll.LongDescription,
			"VoteType":         poll.VoteType,
			"Results":          results,
			"IsOpen":           poll.Open,
			"IsHidden":         poll.Hidden,
			"CanModify":        canModify,
			"Username":         claims.UserInfo.Username,
			"FullName":         claims.UserInfo.FullName,
		})
	}))

	r.POST("/poll/:id/hide", csh.AuthWrapper(func(c *gin.Context) {
		cl, _ := c.Get("cshauth")
		claims := cl.(cshAuth.CSHClaims)

		poll, err := database.GetPoll(c, c.Param("id"))
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		if poll.CreatedBy != claims.UserInfo.Username {
			c.JSON(403, gin.H{"error": "Only the creator can hide a poll result"})
			return
		}

		err = poll.Hide(c)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		pId, _ := primitive.ObjectIDFromHex(poll.Id)
		action := database.Action{
			Id:     "",
			PollId: pId,
			Date:   primitive.NewDateTimeFromTime(time.Now()),
			User:   claims.UserInfo.Username,
			Action: "Hide Results",
		}
		err = database.WriteAction(c, &action)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.Redirect(302, "/results/"+poll.Id)
	}))

	r.POST("/poll/:id/reveal", csh.AuthWrapper(func(c *gin.Context) {
		cl, _ := c.Get("cshauth")
		claims := cl.(cshAuth.CSHClaims)

		poll, err := database.GetPoll(c, c.Param("id"))
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		if poll.CreatedBy != claims.UserInfo.Username {
			c.JSON(403, gin.H{"error": "Only the creator can reveal a poll result"})
			return
		}

		err = poll.Reveal(c)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		pId, _ := primitive.ObjectIDFromHex(poll.Id)
		action := database.Action{
			Id:     "",
			PollId: pId,
			Date:   primitive.NewDateTimeFromTime(time.Now()),
			User:   claims.UserInfo.Username,
			Action: "Reveal Results",
		}
		err = database.WriteAction(c, &action)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.Redirect(302, "/results/"+poll.Id)
	}))

	r.POST("/poll/:id/close", csh.AuthWrapper(func(c *gin.Context) {
		cl, _ := c.Get("cshauth")
		claims := cl.(cshAuth.CSHClaims)
		// This is intentionally left unprotected
		// A user should be able to end their own polls, regardless of if they can vote

		poll, err := database.GetPoll(c, c.Param("id"))
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		if poll.Gatekeep {
			c.JSON(http.StatusForbidden, gin.H{"error": "This poll cannot be closed manually"})
			return
		}

		if poll.CreatedBy != claims.UserInfo.Username {
			if containsString(claims.UserInfo.Groups, "active_rtp") || containsString(claims.UserInfo.Groups, "eboard") {
			} else {
				c.JSON(403, gin.H{"error": "You cannot end this poll."})
				return
			}
		}

		err = poll.Close(c)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		pId, _ := primitive.ObjectIDFromHex(poll.Id)
		action := database.Action{
			Id:     "",
			PollId: pId,
			Date:   primitive.NewDateTimeFromTime(time.Now()),
			User:   claims.UserInfo.Username,
			Action: "Close/End Poll",
		}
		err = database.WriteAction(c, &action)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.Redirect(302, "/results/"+poll.Id)
	}))

	r.GET("/stream/:topic", csh.AuthWrapper(broker.ServeHTTP))

	go broker.Listen()

	r.Run()
}

// canVote determines whether a user can cast a vote.
//
// returns an integer value: 0 is success, 1 is database error, 3 is not active, 4 is gatekept, 9 is already voted
// TODO: use the return value to influence messages shown on results page
func canVote(user cshAuth.CSHUserInfo, poll database.Poll, allowedUsers []string) int {
	// always false if user is not active
	if !slices.Contains(user.Groups, "active") {
		return 3
	}
	voted, err := database.HasVoted(context.Background(), poll.Id, user.Username)
	if err != nil {
		logging.Logger.WithFields(logrus.Fields{"method": "canVote"}).Error(err)
		return 1
	}
	if voted {
		return 9
	}
	if poll.Gatekeep { //if gatekeep is enabled, but they aren't allowed to vote in the poll, false
		if !slices.Contains(allowedUsers, user.Username) {
			return 4
		}
	} //otherwise true
	return 0
}

func uniquePolls(polls []*database.Poll) []*database.Poll {
	var unique []*database.Poll
	for _, poll := range polls {
		if !containsPoll(unique, poll) {
			unique = append(unique, poll)
		}
	}
	return unique
}

func containsPoll(polls []*database.Poll, poll *database.Poll) bool {
	for _, p := range polls {
		if p.Id == poll.Id {
			return true
		}
	}
	return false
}

func hasOption(poll *database.Poll, option string) bool {
	for _, opt := range poll.Options {
		if opt == option {
			return true
		}
	}
	return false
}

func containsString(arr []string, val string) bool {
	for _, a := range arr {
		if a == val {
			return true
		}
	}
	return false
}
