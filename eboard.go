package main

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

var votes map[string]float32
var voters []string

var OPTIONS = []string{"Pass", "Fail", "Abstain"}

func HandleGetEboardVote(c *gin.Context) {
	user := GetUserData(c)
	if IsEboard(user) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "You need to be E-Board to access this page"})
		return
	}
	if votes == nil {
		votes = make(map[string]float32)
	}
	fmt.Println(votes)
	c.HTML(http.StatusOK, "eboard.tmpl", gin.H{
		"Username": user,
		"Voted":    slices.Contains(voters, user.Username),
		"Results":  votes,
		"Options":  OPTIONS,
	})
}

func HandlePostEboardVote(c *gin.Context) {
	user := GetUserData(c)
	if IsEboard(user) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "You need to be E-Board to access this page"})
		return
	}
	if slices.Contains(voters, user.Username) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot vote again!"})
		return
	}
	i := slices.IndexFunc(user.Groups, func(s string) bool {
		return strings.Contains(s, "eboard-")
	})
	if i == -1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You have the eboard group but not an eboard-[position] group. What is wrong with you?"})
		return
	}
	//get the eboard position, count the members, and divide one whole vote by the number of members in the position
	position := user.Groups[i]
	positionMembers := oidcClient.GetOIDCGroup(oidcClient.FindOIDCGroupID(position))
	weight := 1.0 / float32(len(positionMembers))
	fmt.Println(weight)
	//post the vote
	option := c.PostForm("option")
	if option == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You need to pick an option"})
	}
	votes[option] = votes[option] + weight
	voters = append(voters, user.Username)
	c.Redirect(http.StatusFound, "/eboard")
}

func HandleManageEboardVote(c *gin.Context) {
	user := GetUserData(c)
	if !IsEboard(user) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "You need to be E-Board to access this page"})
		return
	}
	if c.PostForm("clear_vote") != "" {
		votes = make(map[string]float32)
		voters = make([]string, 0)
	}
	c.Redirect(http.StatusFound, "/eboard")
}
