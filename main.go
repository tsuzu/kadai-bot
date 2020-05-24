package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"sort"
	"syscall"
	"time"

	"github.com/apognu/gocal"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/xerrors"
)

var (
	client http.Client
)

type Event struct {
	UID string

	Category, Summary, Description string
	Start, End, LastModified       *time.Time
}

type EventModel struct {
	Event       *Event
	LastChecked time.Time
}

func loadCalendar(endpoint string) ([]*Event, error) {
	resp, err := client.Get(endpoint)

	if err != nil {
		return nil, xerrors.Errorf("failed to read calendar: %w", err)
	}
	defer resp.Body.Close()

	parser := gocal.NewParser(resp.Body)
	err = parser.Parse()

	if err != nil {
		panic(xerrors.Errorf("failed to parse calendar: %w", err))
	}

	events := make([]*Event, len(parser.Events))
	for i, event := range parser.Events {
		var category string
		if len(event.Categories) != 0 {
			category = event.Categories[0]
		}
		events[i] = &Event{
			UID:          event.Uid,
			Category:     category,
			Summary:      event.Summary,
			Description:  event.Description,
			Start:        event.Start,
			End:          event.End,
			LastModified: event.LastModified,
		}
	}

	return events, nil
}

type DiscordEvent struct {
	Kind  string // ADD/UPDATE
	Param string
	Event *Event
}

func main() {
	cfg := LoadConfig()

	db, err := bolt.Open(cfg.DBPath, 0770, nil)

	if err != nil {
		panic(err)
	}
	defer db.Close()

	sort.Slice(cfg.NotificationSchedule, func(i, j int) bool {
		return cfg.NotificationSchedule[i] < cfg.NotificationSchedule[j]
	})

	ticker := time.NewTicker(time.Duration(cfg.CheckInterval))
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)

	for {
		select {
		case c := <-ch:
			fmt.Printf("signal received: %v", c)
			break
		case <-ticker.C:
		}

		events, err := loadCalendar(cfg.CalendarEndpoint)

		if err != nil {
			log.Printf("failed to load config: %v", err)

			continue
		}

		discordEvents := make([]*DiscordEvent, 0, 32)
		err = db.Update(func(tx *bolt.Tx) error {
			bucket := tx.Bucket([]byte("events"))
			for _, event := range events {
				b := bucket.Get([]byte(event.UID))
				var de *DiscordEvent
				updated := &EventModel{
					Event:       event,
					LastChecked: time.Now(),
				}

				old := &EventModel{}
				if err := json.Unmarshal(b, old); err != nil {
					de = &DiscordEvent{
						Kind:  "ADD",
						Event: event,
					}
				} else if !reflect.DeepEqual(old.Event, event) {
					de = &DiscordEvent{
						Kind:  "UPDATE",
						Event: event,
					}
				} else {
					for _, n := range cfg.NotificationSchedule {
						ns := time.Duration(n)
						sub := event.End.Sub(updated.LastChecked)

						if event.End.Sub(old.LastChecked) > ns && sub <= ns && sub >= 0 {
							de = &DiscordEvent{
								Kind:  "NOTIFY",
								Param: ns.String(),
								Event: event,
							}
							break
						}
					}
				}

				if de != nil {
					discordEvents = append(discordEvents, de)
				}
			}

			return nil
		})

		if err != nil {
			log.Printf("failed to run transaction: %v", err)

			continue
		}

	}
}
