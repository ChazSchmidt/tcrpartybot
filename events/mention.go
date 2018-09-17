package events

import (
	"encoding/hex"
	"errors"
	"fmt"
	goTwitter "github.com/dghubble/go-twitter/twitter"
	"github.com/ethereum/go-ethereum/crypto"
	"gitlab.com/alpinefresh/tcrpartybot/models"
	"gitlab.com/alpinefresh/tcrpartybot/twitter"
	"log"
	"strings"
)

// ListenForTwitterMentions listens to the Twitter streaming API for any tweets
// mentioning the VIP bot's handle. When received, it sends the events to a
// channel for further processing.
func ListenForTwitterMentions(handle string, eventChan chan<- *Event, errChan chan<- error) {
	client, _, err := twitter.GetClientFromHandle(handle)
	if err != nil {
		errChan <- err
		return
	}

	// Open up a twitter stream filtering for @TCRBotVIP mentions
	mentionString := fmt.Sprintf("@%s", handle)
	params := &goTwitter.StreamFilterParams{
		Track:         []string{mentionString},
		StallWarnings: goTwitter.Bool(false),
	}

	stream, err := client.Streams.Filter(params)
	if err != nil {
		errChan <- err
		return
	}

	// Convert incoming tweets to our native event struct and hand it off to
	// the events channel
	demux := goTwitter.NewSwitchDemux()
	demux.Tweet = func(tweet *goTwitter.Tweet) {
		eventChan <- &Event{
			EventType:    EventTypeMention,
			SourceID:     tweet.User.ID,
			SourceHandle: tweet.User.Name,
			Message:      tweet.Text,
		}
	}

	for message := range stream.Messages {
		demux.Handle(message)
	}
}

func processMention(event *Event, errChan chan<- error) {
	log.Printf("\nReceived mention from %s [%d]: %s", event.SourceHandle, event.SourceID, event.Message)
	// Filter based on let's party
	lower := strings.ToLower(event.Message)
	if strings.Contains(lower, "let's party") {
		processRegistration(event, errChan)
	}
}

func processRegistration(event *Event, errChan chan<- error) {
	// If they already have an account we don't need to continue
	account, _ := models.FindAccountByHandle(event.SourceHandle)
	if account != nil {
		return
	}

	log.Printf("Creating account for %s", event.SourceHandle)
	// Let's create a wallet for them
	key, err := crypto.GenerateKey()
	if err != nil {
		errChan <- err
		return
	}

	address := crypto.PubkeyToAddress(key.PublicKey).Hex()
	privateKey := hex.EncodeToString(key.D.Bytes())

	// Store the association between their handle and their wallet in our db
	account = &models.Account{
		TwitterHandle: event.SourceHandle,
		TwitterID:     event.SourceID,
		ETHAddress:    address,
		ETHPrivateKey: privateKey,
	}
	err = models.CreateAccount(account)

	if err != nil {
		errChan <- err
	}

	// Generate three registration challenges for them
	questions, err := models.FetchRandomRegistrationQuestions(models.RegistrationChallengeCount)

	if err != nil {
		errChan <- err
		return
	}

	// Create a list of challenges for the new user to complete
	challenges := make([]*models.RegistrationChallenge, models.RegistrationChallengeCount)
	for i, question := range questions {
		challenges[i], err = models.CreateRegistrationChallenge(account, &question)

		if err != nil {
			errChan <- err
		}
	}

	// Send them a direct message asking them for the answer to a challenge
	// question
	if len(questions) == 0 {
		errChan <- errors.New("Could not fetch registration question from db")
		return
	}

	firstChallenge := challenges[0]
	twitter.SendDM(account.TwitterHandle, questions[0].Question)

	err = models.MarkRegistrationChallengeSent(firstChallenge.ID)
	if err != nil {
		errChan <- err
	}
}
