package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	cshAuth "github.com/computersciencehouse/csh-auth"
	"github.com/computersciencehouse/vote/logging"
	"github.com/sirupsen/logrus"
)

type OIDCClient struct {
	oidcClientId     string
	oidcClientSecret string

	accessToken  string
	providerBase string
	quit         chan struct{}
}

type OIDCUser struct {
	Uuid     string `json:"id"`
	Username string `json:"username"`
	Gatekeep bool   `json:"result"`
	SlackUID string `json:"slackuid"`
}

func (client *OIDCClient) setupOidcClient(oidcClientId, oidcClientSecret string) {
	client.oidcClientId = oidcClientId
	client.oidcClientSecret = oidcClientSecret
	parse, err := url.Parse(cshAuth.ProviderURI)
	if err != nil {
		logging.Logger.WithFields(logrus.Fields{"method": "setupOidcClient"}).Error(err)
		return
	}
	client.providerBase = parse.Scheme + "://" + parse.Host
	exp := client.getAccessToken()
	ticker := time.NewTicker(time.Duration(exp) * time.Second)
	// this will async get the token
	go func() {
		for {
			select {
			case <-ticker.C:
				client.getAccessToken()
			case <-client.quit:
				ticker.Stop()
				return
			}
		}
	}()
}

func (client *OIDCClient) getAccessToken() int {
	htclient := http.DefaultClient
	//request body
	authData := url.Values{}
	authData.Set("client_id", client.oidcClientId)
	authData.Set("client_secret", client.oidcClientSecret)
	authData.Set("grant_type", "client_credentials")
	resp, err := htclient.PostForm(cshAuth.ProviderURI+"/protocol/openid-connect/token", authData)
	if err != nil {
		logging.Logger.WithFields(logrus.Fields{"method": "setupOidcClient"}).Error(err)
		return 0
	}
	defer resp.Body.Close()
	respData := make(map[string]interface{})
	err = json.NewDecoder(resp.Body).Decode(&respData)
	if err != nil {
		logging.Logger.WithFields(logrus.Fields{"method": "setupOidcClient"}).Error(err)
		return 0
	}
	if respData["error"] != nil {
		logging.Logger.WithFields(logrus.Fields{"method": "setupOidcClient"}).Error(respData)
		return 0
	}
	client.accessToken = respData["access_token"].(string)
	return int(respData["expires_in"].(float64))
}

func (client *OIDCClient) GetActiveUsers() []OIDCUser {
	htclient := &http.Client{}
	//active
	req, err := http.NewRequest("GET", client.providerBase+"/auth/admin/realms/csh/groups/a97a191e-5668-43f5-bc0c-6eefc2b958a7/members", nil)
	if err != nil {
		return nil
	}
	req.Header.Add("Authorization", "Bearer "+client.accessToken)
	resp, err := htclient.Do(req)
	if err != nil {
		logging.Logger.WithFields(logrus.Fields{"method": "GetAllUsers"}).Error(err)
		return nil
	}
	defer resp.Body.Close()
	ret := make([]OIDCUser, 0)
	err = json.NewDecoder(resp.Body).Decode(&ret)
	if err != nil {
		logging.Logger.WithFields(logrus.Fields{"method": "GetAllUsers"}).Error(err)
		return nil
	}
	return ret
}

func (client *OIDCClient) GetUserInfo(user *OIDCUser) {
	htclient := &http.Client{}
	arg := ""
	if len(user.Uuid) == 0 {
		arg = "?username=" + user.Username
	}
	req, err := http.NewRequest("GET", client.providerBase+"/auth/admin/realms/csh/users/"+user.Uuid+arg, nil)
	// also "users/{user-id}/groups"
	if err != nil {
		return
	}
	req.Header.Add("Authorization", "Bearer "+client.accessToken)
	resp, err := htclient.Do(req)
	if err != nil {
		logging.Logger.WithFields(logrus.Fields{"method": "GetUserInfo"}).Error(err)
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if len(arg) > 0 {
		userData := make([]map[string]interface{}, 0)
		err = json.Unmarshal(b, &userData)
		// userdata attributes are a KV pair of string:[]any, this casts attributes, finds the specific attribute, casts it to a list of any, and then pulls the first field since there will only ever be one
		user.SlackUID = userData[0]["attributes"].(map[string]interface{})["slackuid"].([]interface{})[0].(string)
	} else {
		err = json.Unmarshal(b, &user)
	}
	if err != nil {
		logging.Logger.WithFields(logrus.Fields{"method": "GetUserInfo"}).Error(err)
	}
}

func (client *OIDCClient) GetUserGatekeep(user *OIDCUser) {
	htclient := &http.Client{}
	req, err := http.NewRequest("GET", CONDITIONAL_GATEKEEP_URL+user.Username, nil)
	if err != nil {
		return
	}
	req.Header.Add("X-VOTE-TOKEN", VOTE_TOKEN)
	resp, err := htclient.Do(req)
	if err != nil {
		logging.Logger.WithFields(logrus.Fields{"method": "GetUserGatekeep"}).Error(err)
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(b), "Users") {
		logging.Logger.WithFields(logrus.Fields{"method": "GetUserGatekeep"}).Error("Conditional Gatekeep token is incorrect")
		return
	}
	err = json.Unmarshal(b, &user)
	if err != nil {
		logging.Logger.WithFields(logrus.Fields{"method": "GetUserGatekeep"}).Error(err)
	}
}
