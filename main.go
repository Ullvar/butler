package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

type Message struct {
	Id      string
	Labels  []string
	Subject string
	Sender  string
}

type Label struct {
	Id   string
	Name string
}

type Event struct {
	Summary     string
	StartDate   string
	StartTime   time.Time
	EndDateTime string
}

func getClient(config *oauth2.Config) *http.Client {
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(context.Background(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

func read_mail(b []byte, numberOfMessages *int64, labelsToSearch *string) {
	// If modifying these scopes, delete your previously saved token.json.
	config, err := google.ConfigFromJSON(b, gmail.MailGoogleComScope, calendar.CalendarReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := getClient(config)

	ctx := context.Background()
	srv, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to retrieve Gmail client: %v", err)
	}

	labels := []Label{}
	resp, err := srv.Users.Labels.List("me").Do()
	if err != nil {
		log.Fatalf("Unable to retrieve labels: %v", err)
	}
	for _, l := range resp.Labels {
		labels = append(labels, Label{Id: l.Id, Name: l.Name})
	}

	user := "me"
	convertedLabelsToSearch := []string{}
	for _, label := range strings.Split(*labelsToSearch, ",") {
		for _, l := range labels {
			if l.Name == label {
				convertedLabelsToSearch = append(convertedLabelsToSearch, l.Id)
				break
			}
		}
	}
	r, err := srv.Users.Messages.List(user).LabelIds(convertedLabelsToSearch...).MaxResults(*numberOfMessages).Do()
	if err != nil {
		log.Fatalf("Unable to retrieve messages: %v", err)
	}

	messages := []Message{}

	if len(r.Messages) == 0 {
		fmt.Println("No messages found.")
	} else {
		for _, m := range r.Messages {
			msg, err := srv.Users.Messages.Get(user, m.Id).Format("full").Do()
			if err != nil {
				log.Printf("Unable to retrieve message %v: %v", m.Id, err)
				continue
			}
			subject := ""
			from := ""
			for _, header := range msg.Payload.Headers {
				if header.Name == "Subject" {
					subject = header.Value
					break
				}
				if header.Name == "Return-Path" {
					from = strings.ReplaceAll(strings.Split(header.Value, "@")[1], ">", "")
				}
				if header.Name == "From" {
					from = header.Value
				}
			}
			messages = append(messages, Message{Id: m.Id, Labels: msg.LabelIds, Subject: subject, Sender: from})
		}
	}

	fmt.Println("")
	for _, m := range messages {
		fmt.Println("\033[1mSubject:", strings.TrimSpace(m.Subject), "\033[0m")
		fmt.Println("Sender:", m.Sender)
		fmt.Println("")
	}
}

func parseDate(dateStr string) time.Time {
	t, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		// If it fails to parse as date-time, try parsing as date-only
		t, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			fmt.Println("Error parsing date:", err)
			return time.Time{}
		}
	}
	return t
}

func sortEvents(events []Event) {
	sort.Slice(events, func(i, j int) bool {
		return events[i].StartTime.Before(events[j].StartTime)
	})
}

func read_calendar(b []byte) {
	config, err := google.ConfigFromJSON(b, calendar.CalendarReadonlyScope, gmail.MailGoogleComScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := getClient(config)

	ctx := context.Background()
	srv, err := calendar.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to retrieve Calendar client: %v", err)
	}

	calendarId := "primary"
	from := time.Now().Format(time.RFC3339)
	yyyy, mm, dd := time.Now().Date()
	tomorrow := time.Date(yyyy, mm, dd+1, 23, 59, 59, 0, time.Now().Location())
	calendarEvents, err := srv.Events.List(calendarId).TimeMin(from).TimeMax(tomorrow.Format(time.RFC3339)).Do()
	if err != nil {
		log.Fatalf("Unable to retrieve next ten of the user's events: %v", err)
	}

	events := []Event{}

	for _, item := range calendarEvents.Items {
		startDate := ""
		if item.Start != nil && item.Start.DateTime != "" {
			startDate = item.Start.DateTime
		} else if item.Start != nil && item.Start.Date != "" {
			startDate = item.Start.Date
		}
		endDateTime := ""
		if item.End != nil && item.End.DateTime != "" {
			endDateTime = item.End.DateTime
		}

		events = append(events, Event{Summary: item.Summary, StartDate: startDate, StartTime: parseDate(startDate), EndDateTime: endDateTime})
	}

	sortEvents(events)

	todayName := time.Now().Format("Monday")

	fmt.Println("")
	for _, event := range events {
		t, err := time.Parse(time.RFC3339, event.EndDateTime)
		eventDay := event.StartTime.Local().Format("Monday")
		if err != nil {
			if eventDay == todayName {
				fmt.Println("\033[1m***** ", strings.Replace(event.StartTime.Local().Format("Monday"), todayName, "Today", -1), "all day", " *****\033[0m")
				fmt.Println("\033[1m", strings.TrimSpace(event.Summary), "\033[0m")
				fmt.Println("")
			} else {
				fmt.Println("***** ", strings.Replace(event.StartTime.Local().Format("Monday"), todayName, "Today", -1), "all day", " *****")
				fmt.Println(strings.TrimSpace(event.Summary))
				fmt.Println("")
			}
		} else {
			if eventDay == todayName {
				fmt.Println("\033[1m***** ", strings.Replace(event.StartTime.Local().Format("Monday 15:04"), todayName, "Today", -1), "-", t.Local().Format("15:04"), " *****\033[0m")
				fmt.Println("\033[1m", strings.TrimSpace(event.Summary), "\033[0m")
				fmt.Println("")
			} else {
				fmt.Println("***** ", strings.Replace(event.StartTime.Local().Format("Monday 15:04"), todayName, "Today", -1), "-", t.Local().Format("15:04"), " *****")
				fmt.Println(strings.TrimSpace(event.Summary))
				fmt.Println("")
			}
		}
	}
}

func main() {
	var mail = flag.Bool("mail", false, "show mail")
	var calendar = flag.Bool("cal", false, "show calendar")
	var numberOfMessages = flag.Int64("n", 100, "number of messages")
	var labelsToSearch = flag.String("l", "UNREAD", "labels to search (case sensitive)")

	flag.Parse()

	b, err := os.ReadFile("credentials.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	if *mail {
		read_mail(b, numberOfMessages, labelsToSearch)
	} else if *calendar {
		read_calendar(b)
	} else {
		fmt.Println("please specify -mail or -cal")

	}
}
