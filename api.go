package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
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
)

// Gets the number of people eligible to vote in a poll
func GetVoterCount(poll database.Poll) int {
	return len(poll.AllowedUsers)
}

// Calculates the number of votes required for quorum in a poll
func CalculateQuorum(poll database.Poll) int {
	voterCount := GetVoterCount(poll)
	return int(math.Ceil(float64(voterCount) * poll.QuorumType))
}

// GetHomepage Displays the main page of the application, containing a list of all currently open polls
func GetHomepage(c *gin.Context) {
	// This is intentionally left unprotected
	// A user may be unable to vote but should still be able to see a list of polls
	user := GetUserData(c)

	polls, err := database.GetOpenPolls(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	sort.Slice(polls, func(i, j int) bool {
		return polls[i].Id > polls[j].Id
	})

	c.HTML(http.StatusOK, "index.tmpl", gin.H{
		"Polls":    polls,
		"Username": user.Username,
		"FullName": user.FullName,
		"EBoard":   IsEboard(user),
	})
}

// GetClosedPolls Displays a page containing a list of all closed polls that the user created or voted in
func GetClosedPolls(c *gin.Context) {
	cl, _ := c.Get("cshauth")
	claims := cl.(cshAuth.CSHClaims)

	closedPolls, err := database.GetClosedVotedPolls(c, claims.UserInfo.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ownedPolls, err := database.GetClosedOwnedPolls(c, claims.UserInfo.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	closedPolls = append(closedPolls, ownedPolls...)

	sort.Slice(closedPolls, func(i, j int) bool {
		return closedPolls[i].Id > closedPolls[j].Id
	})
	closedPolls = uniquePolls(closedPolls)

	c.HTML(http.StatusOK, "closed.tmpl", gin.H{
		"ClosedPolls": closedPolls,
		"Username":    claims.UserInfo.Username,
		"FullName":    claims.UserInfo.FullName,
	})
}

// GetPollById Retreives the information about a specific poll and displays it on the page, allowing the user to cast a ballot
//
// If the user is not eligible to vote in a particular poll, they are automatically redirected to the results page for that poll
func GetPollById(c *gin.Context) {
	cl, _ := c.Get("cshauth")
	claims := cl.(cshAuth.CSHClaims)
	// This is intentionally left unprotected
	// We will check if a user can vote and redirect them to results if not later

	poll, err := database.GetPoll(c, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// If the user can't vote, just show them results
	if canVote(claims.UserInfo, *poll, poll.AllowedUsers) > 0 || !poll.Open {
		c.Redirect(http.StatusFound, "/results/"+poll.Id)
		return
	}

	writeInAdj := 0
	if poll.AllowWriteIns {
		writeInAdj = 1
	}

	canModify := IsActiveRTP(claims.UserInfo) || IsEboard(claims.UserInfo) || ownsPoll(poll, claims)

	c.HTML(200, "poll.tmpl", gin.H{
		"Id":            poll.Id,
		"Title":         poll.Title,
		"Description":   poll.Description,
		"Options":       poll.Options,
		"PollType":      poll.VoteType,
		"RankedMax":     fmt.Sprint(len(poll.Options) + writeInAdj),
		"AllowWriteIns": poll.AllowWriteIns,
		"CanModify":     canModify,
		"Username":      claims.UserInfo.Username,
		"FullName":      claims.UserInfo.FullName,
	})
}

// CreatePoll Submits the specific details of a new poll that a user wants to create to the database
func CreatePoll(c *gin.Context) {
	cl, _ := c.Get("cshauth")
	claims := cl.(cshAuth.CSHClaims)

	// If user is not active, display the unauthorized screen
	if !IsActive(claims.UserInfo) {
		c.HTML(http.StatusForbidden, "unauthorized.tmpl", gin.H{
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
		Id:            "",
		CreatedBy:     claims.UserInfo.Username,
		Title:         c.PostForm("title"),
		Description:   c.PostForm("description"),
		VoteType:      database.POLL_TYPE_SIMPLE,
		OpenedTime:    time.Now(),
		Open:          true,
		QuorumType:    float64(quorum),
		Gatekeep:      c.PostForm("gatekeep") == "true",
		AllowWriteIns: c.PostForm("allowWriteIn") == "true",
		Hidden:        c.PostForm("hidden") == "true",
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
			if !slices.Contains(poll.Options, "Abstain") && (poll.VoteType == database.POLL_TYPE_SIMPLE) {
				poll.Options = append(poll.Options, "Abstain")
			}
		}
	case "pass-fail":
	default:
		poll.Options = []string{"Pass", "Fail", "Abstain"}
	}
	if poll.Gatekeep {
		if !IsEboard(claims.UserInfo) {
			c.HTML(http.StatusForbidden, "unauthorized.tmpl", gin.H{
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Redirect(http.StatusFound, "/poll/"+pollId)
}

// GetCreatePage Displays the poll creation page to the user
func GetCreatePage(c *gin.Context) {
	cl, _ := c.Get("cshauth")
	claims := cl.(cshAuth.CSHClaims)

	// If the user is not active, display the unauthorized page
	if !IsActive(claims.UserInfo) {
		c.HTML(http.StatusForbidden, "unauthorized.tmpl", gin.H{
			"Username": claims.UserInfo.Username,
			"FullName": claims.UserInfo.FullName,
		})
		return
	}

	c.HTML(http.StatusOK, "create.tmpl", gin.H{
		"Username": claims.UserInfo.Username,
		"FullName": claims.UserInfo.FullName,
		"IsEboard": IsEboard(claims.UserInfo),
	})
}

// GetPollResults Displays the results page for a specific poll
func GetPollResults(c *gin.Context) {
	cl, _ := c.Get("cshauth")
	claims := cl.(cshAuth.CSHClaims)
	// This is intentionally left unprotected
	// A user may be unable to vote but still interested in the results of a poll

	poll, err := database.GetPoll(c, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	results, err := poll.GetResult(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	canModify := IsActiveRTP(claims.UserInfo) || IsEboard(claims.UserInfo) || ownsPoll(poll, claims)

	votesNeededForQuorum := int(poll.QuorumType * float64(len(poll.AllowedUsers)))
	c.HTML(http.StatusOK, "result.tmpl", gin.H{
		"Id":                   poll.Id,
		"Title":                poll.Title,
		"Description":          poll.Description,
		"VoteType":             poll.VoteType,
		"Results":              results,
		"IsOpen":               poll.Open,
		"IsHidden":             poll.Hidden,
		"CanModify":            canModify,
		"CanVote":              canVote(claims.UserInfo, *poll, poll.AllowedUsers),
		"Username":             claims.UserInfo.Username,
		"FullName":             claims.UserInfo.FullName,
		"Gatekeep":             poll.Gatekeep,
		"Quorum":               strconv.FormatFloat(poll.QuorumType*100.0, 'f', 0, 64),
		"EligibleVoters":       poll.AllowedUsers,
		"VotesNeededForQuorum": votesNeededForQuorum,
	})
}

// VoteInPoll Submits a users' vote in a specific poll
func VoteInPoll(c *gin.Context) {
	cl, _ := c.Get("cshauth")
	claims := cl.(cshAuth.CSHClaims)

	poll, err := database.GetPoll(c, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if canVote(claims.UserInfo, *poll, poll.AllowedUsers) > 0 || !poll.Open {
		c.Redirect(http.StatusFound, "/results/"+poll.Id)
		return
	}

	pId, err := primitive.ObjectIDFromHex(poll.Id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if poll.VoteType == database.POLL_TYPE_SIMPLE {
		processSimpleVote(c, poll, pId, claims)
	} else if poll.VoteType == database.POLL_TYPE_RANKED {
		processRankedVote(c, poll, pId, claims)
	} else {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Unknown Poll Type"})
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

	c.Redirect(http.StatusFound, "/results/"+poll.Id)
}

// HidePollResults Makes the results for a particular poll hidden until the poll closes
//
//	If results are hidden, navigating to the results page of that poll will show
//	a page informing the user that the results are hidden, instead of the actual results
func HidePollResults(c *gin.Context) {
	cl, _ := c.Get("cshauth")
	claims := cl.(cshAuth.CSHClaims)

	poll, err := database.GetPoll(c, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if !ownsPoll(poll, claims) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only the creator can hide a poll result"})
		return
	}

	err = poll.Hide(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Redirect(http.StatusFound, "/results/"+poll.Id)
}

// ClosePoll Sets a poll to no longer allow votes to be cast
func ClosePoll(c *gin.Context) {
	cl, _ := c.Get("cshauth")
	claims := cl.(cshAuth.CSHClaims)
	// This is intentionally left unprotected
	// A user should be able to end their own polls, regardless of if they can vote

	poll, err := database.GetPoll(c, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if poll.Gatekeep {
		c.JSON(http.StatusForbidden, gin.H{"error": "This poll cannot be closed manually"})
		return
	}

	if !ownsPoll(poll, claims) {
		if !IsActiveRTP(claims.UserInfo) && !IsEboard(claims.UserInfo) {
			c.JSON(http.StatusForbidden, gin.H{"error": "You cannot end this poll."})
			return
		}
	}

	err = poll.Close(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Redirect(http.StatusFound, "/results/"+poll.Id)
}

// ProcessSimpleVote Parses a simple ballot, validates it, and sends it to the database
func processSimpleVote(c *gin.Context, poll *database.Poll, pId primitive.ObjectID, claims cshAuth.CSHClaims) {
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Option"})
		return
	}
	database.CastSimpleVote(c, &vote, &voter)
}

// ProcessRankedVote Parses the ranked choice ballot, validates it, and then sends it to the database
func processRankedVote(c *gin.Context, poll *database.Poll, pId primitive.ObjectID, claims cshAuth.CSHClaims) {
	vote := database.RankedVote{
		Id:      "",
		PollId:  pId,
		Options: make(map[string]int),
	}
	voter := database.Voter{
		PollId: pId,
		UserId: claims.UserInfo.Username,
	}

	// Populate vote
	for _, option := range poll.Options {
		optionRankStr := c.PostForm(option)
		optionRank, err := strconv.Atoi(optionRankStr)

		if len(optionRankStr) < 1 {
			continue
		}
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "non-number ranking"})
			return
		}

		vote.Options[option] = optionRank
	}

	// process write-in
	if c.PostForm("writeinOption") != "" && c.PostForm("writein") != "" {
		for candidate := range vote.Options {
			if strings.EqualFold(candidate, strings.TrimSpace(c.PostForm("writeinOption"))) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Write-in is already an option"})
				return
			}
		}
		rank, err := strconv.Atoi(c.PostForm("writein"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Write-in rank is not numerical"})
			return
		}
		if rank < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Write-in rank is not positive"})
			return
		}
		vote.Options[c.PostForm("writeinOption")] = rank
	}

	validateRankedBallot(c, vote)

	// Submit Vote
	database.CastRankedVote(c, &vote, &voter)
}

// ValidateRankedBallot Verifies that the ranked choice ballot a user is attempting to submit is a valid ranked choice vote
//
// Specifically, it checks that the ballot is not empty, that there are no duplicate rankings, and that all rankings are between 1 and the total number of candidates
func validateRankedBallot(c *gin.Context, vote database.RankedVote) {
	// Perform checks, vote does not change beyond this
	optionCount := len(vote.Options)
	voted := make([]bool, optionCount)

	// Make sure vote is not empty
	if optionCount == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You did not rank any options"})
		return
	}

	// Duplicate ranks and range check
	for _, rank := range vote.Options {
		if rank > 0 && rank <= optionCount {
			if rank > optionCount {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Rank choice is more than the amount of candidates ranked"})
				return
			}
			if voted[rank-1] {
				c.JSON(http.StatusBadRequest, gin.H{"error": "You ranked two or more candidates at the same level"})
				return
			}
			voted[rank-1] = true
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Candidates chosen must be from 1 to %d", optionCount)})
			return
		}
	}
}

// canVote determines whether a user can cast a vote.
//
// Returns an integer value that indicates what the result is
// 0 -> User is allowed to vote (success)
// 1 -> Database error
// 3 -> User is not active
// 4 -> User doesnt meet gatekeep
// 9 -> User has already voted
func canVote(user cshAuth.CSHUserInfo, poll database.Poll, allowedUsers []string) int {
	// always false if user is not active
	if !DEV_DISABLE_ACTIVE_FILTERS && !IsActive(user) {
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

// ownsPoll Returns whether a user is the owner of a particular poll
func ownsPoll(poll *database.Poll, claims cshAuth.CSHClaims) bool {
	return poll.CreatedBy == claims.UserInfo.Username
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
