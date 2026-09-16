package graph

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/agmonetti/lazyteams/internal/auth"
	"github.com/agmonetti/lazyteams/internal/helpers"
)

func TestDiag(t *testing.T) {
	if os.Getenv("RUN_DIAG") == "" {
		t.Skip("Skipping diag test. Set RUN_DIAG=1 to run.")
	}

	credentials, err := auth.LoadCredentialSet()
	if err != nil {
		t.Fatalf("Auth error: %v", err)
	}

	client := NewClient(credentials)
	selfID, err := client.GetMe()
	if err != nil {
		t.Logf("GetMe error: %v", err)
	} else {
		t.Logf("SelfID: %s", selfID)
		client.SelfID = selfID
	}

	chats, err := client.GetChats()
	if err != nil {
		t.Fatalf("GetChats error: %v", err)
	}

	t.Logf("Found %d chats", len(chats))
	if len(chats) > 16 {
		ch16 := chats[16]
		t.Logf("Testing Chat 16: %s (%s)", ch16.Topic, ch16.ID)
		url := fmt.Sprintf("https://teams.microsoft.com/api/chatsvc/amer/v1/threads/%s/consumptionhorizons", ch16.ID)
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+credentials.Web.Token)
		req.Header.Set("Accept", "application/json")
		resp, err := client.HTTPClient.Do(req)
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Logf("Chat 16 consumptionhorizons status: %d, body: %s", resp.StatusCode, string(b))
		} else {
			t.Logf("Chat 16 error: %v", err)
		}
	}

	// Check chatsvc conversations endpoint
	req, err := http.NewRequest("GET", "https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations?view=msnp24Equivalent&pageSize=30", nil)
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+credentials.Web.Token)
		req.Header.Set("Accept", "application/json")
		resp, err := client.HTTPClient.Do(req)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Logf("ChatSvc /conversations status: %d, body len: %d", resp.StatusCode, len(body))
			if resp.StatusCode == 200 {
				var convResp struct {
					Conversations []struct {
						ID          string `json:"id"`
						LastMessage any    `json:"lastMessage"`
						Properties  any    `json:"properties"`
					} `json:"conversations"`
				}
				_ = json.Unmarshal(body, &convResp)
				t.Logf("Conversations count: %d", len(convResp.Conversations))
				for i, c := range convResp.Conversations {
					var props struct {
						ConsumptionHorizon string `json:"consumptionhorizon"`
						LastImReceivedTime string `json:"lastimreceivedtime"`
					}
					pBytes, _ := json.Marshal(c.Properties)
					_ = json.Unmarshal(pBytes, &props)

					parts := strings.Split(props.ConsumptionHorizon, ";")
					isUnread := false
					if len(parts) >= 2 {
						readTs, _ := strconv.ParseInt(parts[0], 10, 64)
						serverTs, _ := strconv.ParseInt(parts[1], 10, 64)
						if serverTs > readTs {
							isUnread = true
						}
						t.Logf("Conv %d: ID=%s Unread=%v (ReadTs=%d, ServerTs=%d, Diff=%d) LastIm=%s",
							i, c.ID, isUnread, readTs, serverTs, serverTs-readTs, props.LastImReceivedTime)
					} else {
						t.Logf("Conv %d: ID=%s No horizon: %s", i, c.ID, props.ConsumptionHorizon)
					}
				}
			}
		}
	}

	_ = filepath.Join(helpers.ConfigDir(), "tokens.env")
	_ = json.Marshal
}
