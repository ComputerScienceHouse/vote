/*
The MIT License (MIT)

Copyright (c) 2017-2021 Ismael Celis and contributors

Permission is hereby granted, free of charge, to any person obtaining a copy of
this software and associated documentation files (the "Software"), to deal in
the Software without restriction, including without limitation the rights to
use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
the Software, and to permit persons to whom the Software is furnished to do so,
subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
*/

package sse

import (
	"io"
	"log"
	"net/http"
	"time"

	cshAuth "github.com/computersciencehouse/csh-auth"
	"github.com/gin-gonic/gin"
)

const patience time.Duration = time.Second * 1

type (
	NotificationEvent struct {
		EventName string
		Payload   interface{}
	}

	NotifierChan chan NotificationEvent

	Broker struct {

		// Events are pushed to this channel by the main events-gathering routine
		Notifier NotifierChan

		// New client connections
		newClients chan NotifierChan

		// Closed client connections
		closingClients chan NotifierChan

		// Client connections registry
		clients map[NotifierChan]struct{}

		// Callbacks for cross-package access to main package functions
		GetUser           func(*gin.Context) cshAuth.CSHUserInfo
		IsActive          func(cshAuth.CSHUserInfo) bool
		CheckUserGatekeep func(username string) bool
	}
)

func NewBroker(getUser func(*gin.Context) cshAuth.CSHUserInfo, isActive func(cshAuth.CSHUserInfo) bool, checkUserGatekeep func(username string) bool) (broker *Broker) {
	// Instantiate a broker
	return &Broker{
		Notifier:          make(NotifierChan, 1),
		newClients:        make(chan NotifierChan),
		closingClients:    make(chan NotifierChan),
		clients:           make(map[NotifierChan]struct{}),
		GetUser:           getUser,
		IsActive:          isActive,
		CheckUserGatekeep: checkUserGatekeep,
	}
}

func (broker *Broker) ServeHTTP(c *gin.Context) {
	eventName := c.Param("topic")

	if eventName == "new-polls" {
		user := broker.GetUser(c)

		if broker.IsActive(user) {
			if !broker.CheckUserGatekeep(user.Username) {
				eventName = "new-polls-non-gatekeep"
			}
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized SSE connection"})
			c.Abort()
			return
		}
	}

	// Each connection registers its own message channel with the Broker's connections registry
	messageChan := make(NotifierChan)

	// Signal the broker that we have a new connection
	broker.newClients <- messageChan

	// Remove this client from the map of connected clients
	// when this handler exits.
	defer func() {
		broker.closingClients <- messageChan
	}()

	c.Stream(func(w io.Writer) bool {
		// Emit Server Sent Events compatible
		event := <-messageChan

		switch eventName {
		case event.EventName:
			c.SSEvent(event.EventName, event.Payload)
		}

		// Flush the data immediately instead of buffering it for later.
		c.Writer.Flush()

		return true
	})
}

// Listen for new notifications and redistribute them to clients
func (broker *Broker) Listen() {
	for {
		select {
		case s := <-broker.newClients:

			// A new client has connected.
			// Register their message channel
			broker.clients[s] = struct{}{}
			log.Printf("Client added. %d registered clients", len(broker.clients))
		case s := <-broker.closingClients:

			// A client has dettached and we want to
			// stop sending them messages.
			delete(broker.clients, s)
			log.Printf("Removed client. %d registered clients", len(broker.clients))
		case event := <-broker.Notifier:

			// We got a new event from the outside!
			// Send event to all connected clients
			for clientMessageChan := range broker.clients {
				select {
				case clientMessageChan <- event:
				case <-time.After(patience):
					log.Print("Skipping client.")
				}
			}
		}
	}
}
