package events

import (
	"encoding/hex"
	"errors"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/tokenfoundry/tcrpartybot/models"
	"github.com/tokenfoundry/tcrpartybot/twitter"
	"log"
	"strings"
)

const (
	CHALLENGE_QUESTION_COUNT = 3
)

func processMention(event *Event, errChan chan<- error) {
	log.Printf("Received mention from %s: %s", event.SourceHandle, event.Message)
	// Filter based on let's party
	lower := strings.ToLower(event.Message)
	if strings.Contains(lower, "let's party") {
		processRegistration(event, errChan)
	}
}

func processRegistration(event *Event, errChan chan<- error) {
	// If they already have an account we don't need to continue
	account := models.FindAccountByHandle(event.SourceHandle)
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
		ETHAddress:    address,
		ETHPrivateKey: privateKey,
	}
	err = models.CreateAccount(account)

	if err != nil {
		errChan <- err
	}

	// Generate three registration challenges for them
	questions, err := models.FetchRandomRegistrationQuestions(CHALLENGE_QUESTION_COUNT)

	if err != nil {
		errChan <- err
		return
	}

	challenges := make([]*models.RegistrationChallenge, CHALLENGE_QUESTION_COUNT)
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
	twitter.SendDirectMessage(account.TwitterHandle, firstChallenge.RegistrationQuestion.Question)

	err = models.MarkRegistrationChallengeSent(firstChallenge)
	if err != nil {
		errChan <- err
	}
}
