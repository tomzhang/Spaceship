package test

import (
	"encoding/json"
	"spaceship/socketapi"
	"testing"
)

func TestActiveTurnbasedGame(t *testing.T){

	//First, we need to start server with util methods

	//We need two user with their tokens

	//Connect websocket with these tokens

	failChan := make(chan string)
	gameIDChan := make(chan string, 2)
	done := make(chan struct{}, 2)

	server := NewServer(t)
	defer server.Stop()

	firstSesssion := CreateSession(t)

	secondSession := CreateSession(t)

	firstClient, firstOnMessageChan := CreateSocketConn(t, firstSesssion.Token)
	defer firstClient.Close()

	secondClient, secondOnMessageChan := CreateSocketConn(t, secondSession.Token)
	defer secondClient.Close()

	//Client 1
	go func(){

		WriteMessage(failChan, firstClient, &socketapi.Envelope{Cid: "", Message: &socketapi.Envelope_MatchFind{
			MatchFind: &socketapi.MatchFind{
				GameName: "ATGame",
				QueueProperties: map[string]string{"player_count": "2"},
			},
		}})

		var message socketapi.Envelope

		var matchEntry *socketapi.MatchEntry
		for {
			message = ReadMessage(failChan, firstOnMessageChan)

			matchEntry = message.GetMatchEntry()
			if matchEntry == nil {
				failChan <- "Expected message match entry but unrecognized message was returned"
				return
			}

			if matchEntry.State == int32(socketapi.MatchEntry_MATCH_AWAITING_PLAYERS) {
				break
			}
		}

		WriteMessage(failChan, firstClient, &socketapi.Envelope{Cid:"", Message: &socketapi.Envelope_MatchJoin{
			MatchJoin: &socketapi.MatchJoin{
				MatchId: matchEntry.MatchId,
			},
		}})

		message = ReadMessage(failChan, firstOnMessageChan)
		for {
			if message.GetMatchStart() != nil {
				break
			}
			message = ReadMessage(failChan, firstOnMessageChan)
		}
		matchStart := message.GetMatchStart()
		if matchStart == nil {
			failChan <- "Expected message match start but unrecognized message was returned"
			return
		}

		gameIDChan <- matchStart.GameData.Id

		isHomeUser := false
		var gameData PTGameData
		err := json.Unmarshal([]byte(matchStart.GameData.Metadata), &gameData)
		if err != nil {
			failChan <- err.Error()
			return
		}
		if gameData.HomeUser != nil && gameData.HomeUser.UserID == firstSesssion.User.Id {
			isHomeUser = true
		}else if gameData.AwayUser != nil && gameData.AwayUser.UserID == firstSesssion.User.Id {
			isHomeUser = false
		}else{
			failChan <- "User is not assigned as home or away user"
			return
		}

		//Send match update data
		matchUpdateData := PTGameUpdateData{
			FoundWordsLength: 150,
			FoundWordCount: 10,
			TotalDuration: 61,
		}
		matchUpdateRaw, err := json.Marshal(matchUpdateData)
		if err != nil {
			failChan <- err.Error()
			return
		}
		WriteMessage(failChan, firstClient, &socketapi.Envelope{Cid: "", Message: &socketapi.Envelope_GameUpdate{
			GameUpdate: &socketapi.GameUpdate{
				GameID: matchStart.GameData.Id,
				Metadata: string(matchUpdateRaw),
			},
		}})

		message = ReadMessage(failChan, firstOnMessageChan)
		for  {
			if message.GetGameUpdateResp() != nil {
				break
			}
			message = ReadMessage(failChan, firstOnMessageChan)
		}
		matchUpdateResp := message.GetGameUpdateResp()
		if matchUpdateResp == nil {
			failChan <- "Expected message match update resp but unrecognized message was returned"
			return
		}

		err = json.Unmarshal([]byte(matchUpdateResp.GameData.Metadata), &gameData)
		if err != nil {
			failChan <- err.Error()
			return
		}
		userGameData := gameData.HomeUser
		if !isHomeUser {
			userGameData = gameData.AwayUser
		}
		if userGameData.State != PT_GAME_USER_STATE_COMPLETED {
			failChan <- "Expected user game data state is pt_game_user_state_completed but different than this"
			return
		}

		message = ReadMessage(failChan, firstOnMessageChan)
		for  {
			if message.GetGameUpdateResp() != nil {
				break
			}
			message = ReadMessage(failChan, firstOnMessageChan)
		}
		matchUpdateResp = message.GetGameUpdateResp()
		if matchUpdateResp == nil {
			failChan <- "Expected message match update resp but unrecognized message was returned"
			return
		}

		err = json.Unmarshal([]byte(matchUpdateResp.GameData.Metadata), &gameData)
		if err != nil {
			failChan <- err.Error()
			return
		}
		if gameData.HomeUser.State != PT_GAME_USER_STATE_COMPLETED || gameData.AwayUser.State != PT_GAME_USER_STATE_COMPLETED {
			failChan <- "Expected user game data state for both user is pt_game_user_state_completed but different than this"
			return
		}

		done <- struct{}{}
	}()

	go func(){
		WriteMessage(failChan, secondClient, &socketapi.Envelope{Cid: "", Message: &socketapi.Envelope_MatchFind{
			MatchFind: &socketapi.MatchFind{
				GameName: "ATGame",
				QueueProperties: map[string]string{"player_count": "2"},
			},
		}})

		var message socketapi.Envelope

		var matchEntry *socketapi.MatchEntry
		for {
			message = ReadMessage(failChan, secondOnMessageChan)

			matchEntry = message.GetMatchEntry()
			if matchEntry == nil {
				failChan <- "Expected message match entry but unrecognized message was returned"
				return
			}

			if matchEntry.State == int32(socketapi.MatchEntry_MATCH_AWAITING_PLAYERS) {
				break
			}
		}

		WriteMessage(failChan, secondClient, &socketapi.Envelope{Cid:"", Message: &socketapi.Envelope_MatchJoin{
			MatchJoin: &socketapi.MatchJoin{
				MatchId: matchEntry.MatchId,
			},
		}})

		message = ReadMessage(failChan, secondOnMessageChan)
		for {
			if message.GetMatchStart() != nil {
				break
			}
			message = ReadMessage(failChan, secondOnMessageChan)
		}
		matchStart := message.GetMatchStart()
		if matchStart == nil {
			failChan <- "Expected message match start but unrecognized message was returned"
			return
		}

		gameIDChan <- matchStart.GameData.Id

		isHomeUser := false
		var gameData PTGameData
		err := json.Unmarshal([]byte(matchStart.GameData.Metadata), &gameData)
		if err != nil {
			failChan <- err.Error()
			return
		}
		if gameData.HomeUser != nil && gameData.HomeUser.UserID == secondSession.User.Id {
			isHomeUser = true
		}else if gameData.AwayUser != nil && gameData.AwayUser.UserID == secondSession.User.Id {
			isHomeUser = false
		}else{
			failChan <- "User is not assigned as home or away user"
			return
		}

		if (isHomeUser && gameData.AwayUser != nil && gameData.AwayUser.State == PT_GAME_USER_STATE_COMPLETED) || (!isHomeUser && gameData.HomeUser != nil && gameData.HomeUser.State == PT_GAME_USER_STATE_COMPLETED){
			//Don't need to wait broadcast message about update
		}else{

			message = ReadMessage(failChan, secondOnMessageChan)
			for  {
				if message.GetGameUpdateResp() != nil {
					break
				}
				message = ReadMessage(failChan, secondOnMessageChan)
			}
			matchUpdateResp := message.GetGameUpdateResp()
			if matchUpdateResp == nil {
				failChan <- "Expected message match update resp but unrecognized message was returned"
				return
			}

			err = json.Unmarshal([]byte(matchUpdateResp.GameData.Metadata), &gameData)
			if err != nil {
				failChan <- err.Error()
				return
			}

		}

		otherUserGameData := gameData.AwayUser
		if !isHomeUser {
			otherUserGameData = gameData.HomeUser
		}
		if otherUserGameData.State != PT_GAME_USER_STATE_COMPLETED {
			failChan <- "Expected user game data state is pt_game_user_state_completed but different than this"
			return
		}

		//Send match update data
		matchUpdateData := PTGameUpdateData{
			FoundWordsLength: 170,
			FoundWordCount: 16,
			TotalDuration: 91,
		}
		matchUpdateRaw, err := json.Marshal(matchUpdateData)
		if err != nil {
			failChan <- err.Error()
			return
		}
		WriteMessage(failChan, secondClient, &socketapi.Envelope{Cid: "", Message: &socketapi.Envelope_GameUpdate{
			GameUpdate: &socketapi.GameUpdate{
				GameID: matchStart.GameData.Id,
				Metadata: string(matchUpdateRaw),
			},
		}})

		message = ReadMessage(failChan, secondOnMessageChan)
		for  {
			if message.GetGameUpdateResp() != nil {
				break
			}
			message = ReadMessage(failChan, secondOnMessageChan)
		}
		matchUpdateResp := message.GetGameUpdateResp()
		if matchUpdateResp == nil {
			failChan <- "Expected message match update resp but unrecognized message was returned"
			return
		}

		err = json.Unmarshal([]byte(matchUpdateResp.GameData.Metadata), &gameData)
		if err != nil {
			failChan <- err.Error()
			return
		}
		userGameData := gameData.HomeUser
		if !isHomeUser {
			userGameData = gameData.AwayUser
		}
		if userGameData.State != PT_GAME_USER_STATE_COMPLETED {
			failChan <- "Expected user game data state is pt_game_user_state_completed but different than this"
			return
		}

		done <- struct{}{}
	}()


	prevGameID := ""
	for i:=0; i < 2; i++ {
		select {
		case err := <-failChan:
			t.Fatal(err)
			break
		case gameID := <-gameIDChan:
			if prevGameID == "" {
				prevGameID = gameID
			}else{
				newGameID := gameID
				if prevGameID != newGameID {
					t.Fatal("Game IDs are not equal test failed", prevGameID, newGameID)
				}
			}
		}
	}

	for i:=0; i<2; i++ {
		<-done
	}

}
