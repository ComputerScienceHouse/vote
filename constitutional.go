package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/computersciencehouse/vote/database"
	"github.com/computersciencehouse/vote/logging"
	"github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

type SlackData struct {
	AnnouncementsChannel string
	Client               *socketmode.Client
}

var slackData = SlackData{}

func InitConstitution() {
	slackData.AnnouncementsChannel = os.Getenv("VOTE_ANNOUNCEMENTS_CHANNEL_ID")
	if slackData.AnnouncementsChannel == "" {
		logging.Logger.WithFields(logrus.Fields{"method": "InitConstitution"}).Error("No announcements channel ID specified")
	}

	appToken := os.Getenv("VOTE_SLACK_APP_TOKEN")
	if appToken == "" {
		logging.Logger.WithFields(logrus.Fields{"method": "InitSlack"}).Error("No Slack app token specified")
	}
	if !strings.HasPrefix(appToken, "xapp-") {
		logging.Logger.WithFields(logrus.Fields{"method": "InitConstitution"}).Error("Invalid Slack app token (should have prefix \"xapp-\".")
	}

	botToken := os.Getenv("VOTE_SLACK_BOT_TOKEN")
	if botToken == "" {
		logging.Logger.WithFields(logrus.Fields{"method": "InitConstitution"}).Error("No Slack bot token specified")
	}
	if !strings.HasPrefix(botToken, "xoxb-") {
		logging.Logger.WithFields(logrus.Fields{"method": "InitConstitution"}).Error("Invalid Slack bot token (should have prefix \"xoxb-\".")
	}

	api := slack.New(botToken, slack.OptionAppLevelToken(appToken))
	slackData.Client = socketmode.New(api)
	t := time.Now()
	startOfDay := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
	nextMidnight := startOfDay.AddDate(0, 0, 1)
	fmt.Println(nextMidnight)
	fmt.Println(time.Until(nextMidnight))
	ticker := time.NewTicker(time.Until(nextMidnight))
	first := true
	go func() {
		for {
			select {
			case <-ticker.C:
				EvaluatePolls()
				if first {
					ticker.Reset(24 * time.Hour)
					first = false
				}
			case <-oidcClient.quit:
				ticker.Stop()
				return
			}
		}
	}()
}

// GetEligibleVoters returns a string slice of usernames of eligible voters
func GetEligibleVoters() []string {
	ret := make([]string, 0)
	allactive := oidcClient.GetActiveUsers()
	//todo: figure out why this is slow as FORK
	for _, a := range allactive {
		oidcClient.GetUserGatekeep(&a)
		if a.Gatekeep {
			ret = append(ret, a.Username)
		}
	}
	return ret
}

func EvaluatePolls() {
	ctx := context.Background()
	polls, err := database.GetOpenGatekeepPolls(ctx)
	var closedPolls []*database.Poll
	if err != nil {
		logging.Logger.WithFields(logrus.Fields{"method": "EvaluatePolls getOpen"}).Error(err)
		return
	}
	for _, poll := range polls {
		now := time.Now()
		// if OpenedTime + 1 day later is before today, it's been open for less than 24 hours, and we will re-evaluate next run
		// if after, it's been more than 24 hours
		// we still won't close until 48, but we want to start messaging at 24
		if poll.OpenedTime.AddDate(0, 0, 1).After(now) {
			continue
		}

		quorum := CalculateQuorum(*poll)

		notVoted := make([]*OIDCUser, 0)
		votedCount := 0
		// check all voters to see if they have voted
		if poll.AllowedUsers == nil {
			logging.Logger.WithFields(logrus.Fields{"method": "EvaluatePolls checkQuorum"}).Error(
				"Users allowed to vote is nil for \"" + poll.ShortDescription + "\" !! This should not happen!!")
			continue
		}
		for _, user := range poll.AllowedUsers {
			voted, err := database.HasVoted(ctx, poll.Id, user)
			if err != nil {
				logging.Logger.WithFields(logrus.Fields{"method": "EvaluatePolls hasVoted"}).Error(err)
				continue
			}
			if voted {
				votedCount = votedCount + 1
				continue
			}
			notVoted = append(notVoted, &OIDCUser{Username: user})
		}
		pollLink := VOTE_HOST + "/poll/" + poll.Id
		// quorum not met
		if votedCount < quorum {
			for _, user := range notVoted {
				oidcClient.GetUserInfo(user)
				_, _, err = slackData.Client.PostMessage(user.SlackUID,
					slack.MsgOptionText(
						"Hello, you have not yet voted on \""+poll.ShortDescription+"\". We have not yet hit quorum"+
							" and we need YOU :index_pointing_at_the_viewer: to complete your responsibility as a "+
							"member of house and vote. \n"+pollLink+"\nThank you!", false))
				if err != nil {
					logging.Logger.WithFields(logrus.Fields{"method": "EvaluatePolls dm"}).Error(err)
					continue
				}
			}
			continue
		}
		// close poll after 48 hours
		if poll.OpenedTime.AddDate(0, 0, 2).After(now) {
			continue
		}
		// we close the poll here
		err = poll.Close(ctx)
		fmt.Println("Time reached, closing poll " + poll.ShortDescription)
		if err != nil {
			logging.Logger.WithFields(logrus.Fields{"method": "EvaluatePolls close"}).Error(err)
			continue
		}
		closedPolls = append(closedPolls, poll)
	}

	if len(closedPolls) == 0 {
		return;
	}

	// Announce all closed polls with one announcement.
	announceStr := ""
	if len(closedPolls) > 1 {
		announceStr += "Updates on " + strconv.Itoa(len(closedPolls)) + " polls:\n\n"
	}
	for _, poll := range closedPolls {
		pollLink := VOTE_HOST + "/poll/" + poll.Id
		announceStr += "The vote \"" + poll.ShortDescription + "\" has closed."
		if !poll.Hidden {
			announceStr += " Check out the results at " + pollLink
		} else {
			announceStr += " Results will be posted shortly."
		}
		announceStr += "\n"
	}
	_, _, _, err = slackData.Client.SendMessage(slackData.AnnouncementsChannel,
		slack.MsgOptionText(announceStr, false))
	if err != nil {
		logging.Logger.WithFields(logrus.Fields{"method": "EvaluatePolls announce"}).Error(err)
	}
}
