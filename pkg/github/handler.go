package github

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"github.com/google/go-github/github"
)

// formatRequest generates ascii representation of a request
// from: https://medium.com/doing-things-right/pretty-printing-http-requests-in-golang-a918d5aaa000
func formatRequest(r *http.Request) string {
	// Create return string
	var request []string
	// Add the request string
	url := fmt.Sprintf("%v %v %v", r.Method, r.URL, r.Proto)
	request = append(request, url)
	// Add the host
	request = append(request, fmt.Sprintf("Host: %v", r.Host))
	// Loop through headers
	for name, headers := range r.Header {
		name = strings.ToLower(name)
		for _, h := range headers {
			request = append(request, fmt.Sprintf("%v: %v", name, h))
		}
	}

	// If this is a POST, add post data
	if r.Method == "POST" {
		r.ParseForm()
		request = append(request, "\n")
		request = append(request, r.Form.Encode())
	}
	// Return the request as a string
	return strings.Join(request, "\n")
}

func Handler(w http.ResponseWriter, r *http.Request) {
	payload, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Printf("ERROR: no payload: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// TODO(mattmoor): This should be:
	//     eventType := github.WebHookType(r)
	// https://github.com/knative/eventing-sources/issues/120
	// HACK HACK HACK
	parts := strings.Split(r.Header.Get("ce-eventtype"), ".")
	eventType := parts[len(parts)-1]

	event, err := github.ParseWebHook(eventType, payload)
	if err != nil {
		log.Printf("ERROR: unable to parse webhook: %v\n%v", err, formatRequest(r))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	handleErr := func(event interface{}, err error) {
		if err == nil {
			fmt.Fprintf(w, "Handled %T", event)
			return
		}
		log.Printf("Error handling %T: %v", event, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// The set of events here should line up with what is in
	//   config/one-time/github-source.yaml
	switch event := event.(type) {
	case *github.PullRequestEvent:
		handleErr(event, HandlePullRequest(event))
	case *github.IssuesEvent:
		handleErr(event, HandleIssues(event))
	default:
		log.Printf("Unrecognized event: %T", event)
		http.Error(w, "Unknown event", http.StatusBadRequest)
		return
	}
}

func HandleIssues(ie *github.IssuesEvent) error {
	if ie.GetIssue().Milestone != nil {
		log.Printf("Issue #%v already has a milestone.", ie.GetIssue().GetNumber())
		return nil
	}
	if ie.GetIssue().GetState() == "closed" {
		log.Printf("Issue #%v is closed.", ie.GetIssue().GetNumber())
		return nil
	}

	return needsTriage(ie.Repo.Owner.GetLogin(), ie.Repo.GetName(), ie.GetIssue().GetNumber())
}

func HandlePullRequest(pre *github.PullRequestEvent) error {
	if pre.GetPullRequest().Milestone != nil {
		log.Printf("PR #%v already has a milestone.", pre.GetNumber())
		return nil
	}
	if pre.GetPullRequest().GetState() == "closed" {
		log.Printf("PR #%v is closed.", pre.GetNumber())
		return nil
	}

	return needsTriage(pre.Repo.Owner.GetLogin(), pre.Repo.GetName(), pre.GetNumber())
}

func needsTriage(owner, repo string, number int) error {
	ctx := context.Background()
	ghc := GetClient(ctx)
	m, err := getOrCreateMilestone(ctx, ghc, owner, repo, "Needs Triage")
	if err != nil {
		return err
	}

	_, _, err = ghc.Issues.Edit(ctx, owner, repo, number, &github.IssueRequest{
		Milestone: m.Number,
	})
	return err
}

func getOrCreateMilestone(ctx context.Context, client *github.Client,
	owner, repo, title string) (*github.Milestone, error) {
	// Walk the pages of milestones looking for one matching our title.
	lopt := &github.MilestoneListOptions{}
	for {
		ms, resp, err := client.Issues.ListMilestones(ctx, owner, repo, lopt)
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			if m.GetTitle() == title {
				return m, nil
			}
		}
		if lopt.Page == resp.NextPage {
			break
		}
		lopt.Page = resp.NextPage
	}

	m, _, err := client.Issues.CreateMilestone(ctx, owner, repo, &github.Milestone{
		Title: &title,
	})
	return m, err
}
