package graph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type AssignmentFile struct {
	ID      string
	Name    string
	FileUrl string
}

type Assignment struct {
	ID                 string
	SubmissionID       string
	DisplayName        string
	ClassID            string
	DueDateTime        time.Time
	Status             string
	IsCompleted        bool
	AnySubmittedState  bool
	SubmittedDateTime  time.Time
	Instructions       string
	WebUrl             string
	SubmissionStatus   string
	ResourcesFolderUrl string
	MyFiles            []AssignmentFile
	RefFiles           []AssignmentFile
}

func (c *Client) FetchAssignments() ([]Assignment, error) {
	if c.EduToken == "" {
		return nil, fmt.Errorf("EDU_TOKEN not configured")
	}

	url := "https://assignments.edu.cloud.microsoft/api/v1.0/edu/me/work" +
		"?$top=50" +
		"&$expand=submissions,categories,submissionAggregates"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.EduToken)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:138.0) Gecko/20100101 Firefox/138.0")
	req.Header.Set("Origin", "https://teams.microsoft.com")
	req.Header.Set("Referer", "https://teams.microsoft.com/")
	req.Header.Set("X-Ms-Client-Type", "web")
	req.Header.Set("X-Ms-ClientType", "web")
	if c.EduCookie != "" {
		req.Header.Set("Cookie", c.EduCookie)
	} else if c.Cookie != "" {
		req.Header.Set("Cookie", c.Cookie)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error fetching assignments: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == 401 {
			return nil, fmt.Errorf("401")
		}
		return nil, fmt.Errorf("assignments API error (%d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response: %w", err)
	}

	var raw struct {
		Value []struct {
			ID                string `json:"id"`
			DisplayName       string `json:"displayName"`
			ClassID           string `json:"classId"`
			Status            string `json:"status"`
			IsCompleted       bool   `json:"isCompleted"`
			AnySubmittedState bool   `json:"anySubmittedState"`
			DueDateTime       string `json:"dueDateTime"`
			WebUrl            string `json:"webUrl"`
			Instructions      *struct {
				Content string `json:"content"`
			} `json:"instructions"`
			Submissions []struct {
				ID                string  `json:"id"`
				Status            string  `json:"status"`
				SubmittedDateTime *string `json:"submittedDateTime"`
				Resources         []struct {
					ID       string `json:"id"`
					Resource struct {
						DisplayName string `json:"displayName"`
						FileUrl     string `json:"fileUrl"`
					} `json:"resource"`
				} `json:"resources"`
				SubmittedResources []struct {
					ID       string `json:"id"`
					Resource struct {
						DisplayName string `json:"displayName"`
						FileUrl     string `json:"fileUrl"`
					} `json:"resource"`
				} `json:"submittedResources"`
			} `json:"submissions"`
		} `json:"value"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("error parsing assignments: %w", err)
	}

	assignments := make([]Assignment, 0, len(raw.Value))
	for _, v := range raw.Value {
		a := Assignment{
			ID:               v.ID,
			DisplayName:      v.DisplayName,
			ClassID:          v.ClassID,
			Status:           v.Status,
			IsCompleted:      v.IsCompleted,
			WebUrl:           v.WebUrl,
			SubmissionStatus: "working", // default fallback
		}
		if v.Instructions != nil {
			a.Instructions = cleanHTML(v.Instructions.Content)
		}

		if len(v.Submissions) > 0 {
			a.SubmissionID = v.Submissions[0].ID
			sub := v.Submissions[0]

			// Ensure the status is lowercase for validations
			cleanStatus := strings.ToLower(sub.Status)
			if cleanStatus != "" {
				a.SubmissionStatus = cleanStatus
			}

			if cleanStatus == "submitted" || cleanStatus == "returned" || cleanStatus == "reassigned" {
				a.AnySubmittedState = true
			}
			if sub.SubmittedDateTime != nil && *sub.SubmittedDateTime != "" {
				if t, err := time.Parse(time.RFC3339, *sub.SubmittedDateTime); err == nil {
					a.SubmittedDateTime = t
				}
			}

			for _, r := range sub.Resources {
				if r.Resource.DisplayName != "" {
					fu := r.Resource.FileUrl
					if fu == "" {
						fu = a.WebUrl
					}
					a.RefFiles = append(a.RefFiles, AssignmentFile{
						ID:      r.ID,
						Name:    r.Resource.DisplayName,
						FileUrl: fu,
					})
				}
			}

			for _, sr := range sub.SubmittedResources {
				if sr.Resource.DisplayName != "" {
					fu := sr.Resource.FileUrl
					if fu == "" {
						fu = a.WebUrl
					}
					a.MyFiles = append(a.MyFiles, AssignmentFile{
						ID:      sr.ID,
						Name:    sr.Resource.DisplayName,
						FileUrl: fu,
					})
				}
			}
		}

		if v.DueDateTime != "" {
			if t, err := time.Parse(time.RFC3339, v.DueDateTime); err == nil {
				a.DueDateTime = t
			}
		}
		assignments = append(assignments, a)
	}

	return assignments, nil
}

func (c *Client) FetchAssignmentFiles(classID, assignmentID string) (refFiles, myFiles []AssignmentFile, resourcesFolderUrl string, err error) {
	url := fmt.Sprintf(
		"https://assignments.edu.cloud.microsoft/api/v1.0/edu/classes/%s/assignments/%s/submissions?$expand=resources($expand=dependentResources),submittedResources($expand=dependentResources),outcomes&$top=1000",
		classID, assignmentID,
	)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, "", err
	}

	req.Header.Set("Authorization", "Bearer "+c.EduToken)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:138.0) Gecko/20100101 Firefox/138.0")
	req.Header.Set("Origin", "https://teams.microsoft.com")
	req.Header.Set("Referer", "https://teams.microsoft.com/")
	req.Header.Set("X-Ms-Client-Type", "web")
	req.Header.Set("X-Ms-ClientType", "web")
	if c.EduCookie != "" {
		req.Header.Set("Cookie", c.EduCookie)
	} else if c.Cookie != "" {
		req.Header.Set("Cookie", c.Cookie)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, nil, "", fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, "", err
	}

	var res struct {
		Value []struct {
			ResourcesFolderUrl string `json:"resourcesFolderUrl"`
			Resources          []struct {
				ID       string `json:"id"`
				Resource struct {
					DisplayName string `json:"displayName"`
					FileUrl     string `json:"fileUrl"`
				} `json:"resource"`
			} `json:"resources"`
			SubmittedResources []struct {
				ID       string `json:"id"`
				Resource struct {
					DisplayName string `json:"displayName"`
					FileUrl     string `json:"fileUrl"`
				} `json:"resource"`
			} `json:"submittedResources"`
		} `json:"value"`
	}

	if err := json.Unmarshal(body, &res); err != nil {
		return nil, nil, "", err
	}

	if len(res.Value) > 0 {
		sub := res.Value[0]
		resourcesFolderUrl = sub.ResourcesFolderUrl
		for _, r := range sub.Resources {
			if r.Resource.DisplayName != "" {
				refFiles = append(refFiles, AssignmentFile{
					ID:      r.ID,
					Name:    r.Resource.DisplayName,
					FileUrl: r.Resource.FileUrl,
				})
			}
		}
		for _, sr := range sub.SubmittedResources {
			if sr.Resource.DisplayName != "" {
				myFiles = append(myFiles, AssignmentFile{
					ID:      sr.ID,
					Name:    sr.Resource.DisplayName,
					FileUrl: sr.Resource.FileUrl,
				})
			}
		}
	}

	return refFiles, myFiles, resourcesFolderUrl, nil
}

// RegisterAssignmentResource registers an uploaded file as a submission resource.
func (c *Client) RegisterAssignmentResource(classID, assignmentID, submissionID, fileURL, displayName string) error {
	endpoint := fmt.Sprintf(
		"https://assignments.edu.cloud.microsoft/api/v1.0/edu/classes/%s/assignments/%s/submissions/%s/resources",
		classID, assignmentID, submissionID,
	)
	body := map[string]interface{}{
		"resource": map[string]interface{}{
			"@odata.type": "#microsoft.education.assignments.api.educationFileResource",
			"fileUrl":     fileURL,
			"displayName": displayName,
		},
	}
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", endpoint, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+c.EduToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register resource failed %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// SubmitAssignment submits a student's work for an assignment.
func (c *Client) SubmitAssignment(classID, assignmentID, submissionID string) error {
	endpoint := fmt.Sprintf(
		"https://assignments.edu.cloud.microsoft/api/v1.0/edu/classes/%s/assignments/%s/submissions/%s/submit",
		classID, assignmentID, submissionID,
	)
	req, _ := http.NewRequest("POST", endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+c.EduToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("submit failed %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// UndoSubmitAssignment reverses a student's submission.
func (c *Client) UndoSubmitAssignment(classID, assignmentID, submissionID string) error {
	endpoint := fmt.Sprintf(
		"https://assignments.edu.cloud.microsoft/api/v1.0/edu/classes/%s/assignments/%s/submissions/%s/undoSubmit",
		classID, assignmentID, submissionID,
	)
	req, _ := http.NewRequest("POST", endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+c.EduToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("undo submit failed %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// RemoveAssignmentResource deletes a resource from a submission.
func (c *Client) RemoveAssignmentResource(classID, assignmentID, submissionID, resourceID string) error {
	endpoint := fmt.Sprintf(
		"https://assignments.edu.cloud.microsoft/api/v1.0/edu/classes/%s/assignments/%s/submissions/%s/resources/%s",
		classID, assignmentID, submissionID, resourceID,
	)
	req, _ := http.NewRequest("DELETE", endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+c.EduToken)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("remove resource failed %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func AssignmentStatusLabel(a Assignment) string {
	switch a.SubmissionStatus {
	case "submitted":
		return "[TURNED IN] "
	case "returned":
		return "[RETURNED]  "
	case "reassigned":
		return "[REVISION]  "
	case "working":
		if !a.DueDateTime.IsZero() && a.DueDateTime.Before(time.Now()) {
			return "[OVERDUE]   "
		}
		return "[PENDING]   "
	default:
		return "[ASSIGNMENT]"
	}
}
