package main

import (
	"encoding/json"
	"fmt"
	csh_auth "github.com/computersciencehouse/csh-auth"
	"github.com/computersciencehouse/vote/database"
	"github.com/computersciencehouse/vote/sse"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"html/template"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

func inc(x int) string {
	return strconv.Itoa(x + 1)
}

func main() {
	r := gin.Default()
	r.StaticFS("/static", http.Dir("static"))
	r.SetFuncMap(template.FuncMap{
		"inc": inc,
	})
	r.LoadHTMLGlob("templates/*")
	broker := sse.NewBroker()

	csh := csh_auth.CSHAuth{}
	csh.Init(
		os.Getenv("VOTE_OIDC_ID"),
		os.Getenv("VOTE_OIDC_SECRET"),
		os.Getenv("VOTE_JWT_SECRET"),
		os.Getenv("VOTE_STATE"),
		os.Getenv("VOTE_HOST"),
		os.Getenv("VOTE_HOST")+"/auth/callback",
		os.Getenv("VOTE_HOST")+"/auth/login",
		[]string{"profile", "email", "groups"},
	)

	r.GET("/auth/login", csh.AuthRequest)
	r.GET("/auth/callback", csh.AuthCallback)
	r.GET("/auth/logout", csh.AuthLogout)

	r.GET("/", csh.AuthWrapper(func(c *gin.Context) {
		cl, _ := c.Get("cshauth")
		claims := cl.(csh_auth.CSHClaims)
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
		claims := cl.(csh_auth.CSHClaims)
		if !canVote(claims.UserInfo.Groups) {
			c.HTML(403, "unauthorized.tmpl", gin.H{
				"Username": claims.UserInfo.Username,
				"FullName": claims.UserInfo.FullName,
			})
			return
		}

		c.HTML(200, "create.tmpl", gin.H{
			"Username": claims.UserInfo.Username,
			"FullName": claims.UserInfo.FullName,
		})
	}))

	r.POST("/create", csh.AuthWrapper(func(c *gin.Context) {
		cl, _ := c.Get("cshauth")
		claims := cl.(csh_auth.CSHClaims)
		if !canVote(claims.UserInfo.Groups) {
			c.HTML(403, "unauthorized.tmpl", gin.H{
				"Username": claims.UserInfo.Username,
				"FullName": claims.UserInfo.FullName,
			})
			return
		}

		poll := &database.Poll{
			Id:               "",
			CreatedBy:        claims.UserInfo.Username,
			ShortDescription: c.PostForm("shortDescription"),
			LongDescription:  c.PostForm("longDescription"),
			VoteType:         database.POLL_TYPE_SIMPLE,
			Open:             true,
			Hidden:           false,
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
			for _, opt := range strings.Split(c.PostForm("customOptions"), ",") {
				poll.Options = append(poll.Options, strings.TrimSpace(opt))
				if !containsString(poll.Options, "Abstain") && (poll.VoteType == database.POLL_TYPE_SIMPLE) {
					poll.Options = append(poll.Options, "Abstain")
				}
			}
		case "pass-fail":
		default:
			poll.Options = []string{"Pass", "Fail", "Abstain"}
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
		claims := cl.(csh_auth.CSHClaims)
		// This is intentionally left unprotected
		// We will check if a user can vote and redirect them to results if not later

		poll, err := database.GetPoll(c, c.Param("id"))
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		// If the user can't vote, just show them results
		if !canVote(claims.UserInfo.Groups) {
			c.Redirect(302, "/results/"+poll.Id)
			return
		}

		if !poll.Open {
			c.Redirect(302, "/results/"+poll.Id)
			return
		}

		hasVoted, err := database.HasVoted(c, poll.Id, claims.UserInfo.Username)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if hasVoted {
			c.Redirect(302, "/results/"+poll.Id)
			return
		}

		writeInAdj := 0
		if poll.AllowWriteIns {
			writeInAdj = 1
		}

		canModify := containsString(claims.UserInfo.Groups, "active_rtp") || containsString(claims.UserInfo.Groups, "eboard") || poll.CreatedBy == claims.UserInfo.Username

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
		claims := cl.(csh_auth.CSHClaims)
		if !canVote(claims.UserInfo.Groups) {
			c.HTML(403, "unauthorized.tmpl", gin.H{
				"Username": claims.UserInfo.Username,
				"FullName": claims.UserInfo.FullName,
			})
			return
		}

		poll, err := database.GetPoll(c, c.Param("id"))
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		hasVoted, err := database.HasVoted(c, poll.Id, claims.UserInfo.Username)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if hasVoted || !poll.Open {
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
			for _, opt := range poll.Options {
				if c.PostForm(opt) != "" {
					rank, err := strconv.Atoi(c.PostForm(opt))
					if err != nil {
						c.JSON(500, gin.H{"error": "error parsing votes"})
					}
					if rank > 0 {
						vote.Options[opt] = rank
					}
				}
			}
			if c.PostForm("writeinOption") != "" && c.PostForm("writein") != "" {
				rank, err := strconv.Atoi(c.PostForm("writein"))
				if err != nil {
					c.JSON(500, gin.H{"error": "error parsing votes"})
				}
				if rank > 0 {
					vote.Options[c.PostForm("writeinOption")] = rank
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
		claims := cl.(csh_auth.CSHClaims)
		// This is intentionally left unprotected
		// A user may be unable to vote but still interested in the results of a poll

		poll, err := database.GetPoll(c, c.Param("id"))
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		if poll.Hidden && poll.CreatedBy != claims.UserInfo.Username {
			c.JSON(403, gin.H{"Success": "Result Hidden"})
			return
		}

		results, err := poll.GetResult(c)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		canModify := containsString(claims.UserInfo.Groups, "active_rtp") || containsString(claims.UserInfo.Groups, "eboard") || poll.CreatedBy == claims.UserInfo.Username

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
		claims := cl.(csh_auth.CSHClaims)

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
		claims := cl.(csh_auth.CSHClaims)

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
		claims := cl.(csh_auth.CSHClaims)
		// This is intentionally left unprotected
		// A user should be able to end their own polls, regardless of if they can vote

		poll, err := database.GetPoll(c, c.Param("id"))

		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
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

func canVote(groups []string) bool {
	var active, fallCoop, springCoop bool
	for _, group := range groups {
		if group == "active" {
			active = true
		}
		if group == "fall_coop" {
			fallCoop = true
		}
		if group == "spring_coop" {
			springCoop = true
		}
	}

	if time.Now().Month() > time.July {
		return active && !fallCoop
	} else {
		return active && !springCoop
	}
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
