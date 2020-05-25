package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/apognu/gocal"
	"github.com/bwmarrin/discordgo"
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
		return nil, xerrors.Errorf("failed to parse calendar: %w", err)
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
	Param interface{}
	Event *Event
}

func main() {
	cfg := LoadConfig()

	db, err := bolt.Open(cfg.DBPath, 0770, nil)

	if err != nil {
		panic(err)
	}
	defer db.Close()

	session, err := discordgo.New("Bot " + cfg.Discord.Token)

	if err != nil {
		panic(err)
	}

	err = session.Open()
	if err != nil {
		panic(err)
	}
	defer session.Close()

	if len(cfg.Discord.GuildID) == 0 {
		guilds, err := session.UserGuilds(1, "", "")

		if err != nil {
			panic(err)
		}

		if len(guilds) == 0 {
			panic("no available guilds")
		}

		cfg.Discord.GuildID = guilds[0].ID
		fmt.Printf("Discord guild %s(%s) is automatically selected\n", guilds[0].Name, guilds[0].ID)
	}

	sort.Slice(cfg.Notification.Schedules, func(i, j int) bool {
		return cfg.Notification.Schedules[i] < cfg.Notification.Schedules[j]
	})

	ticker := time.NewTicker(time.Duration(cfg.CheckInterval))
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)

	first := make(chan struct{}, 1)
	first <- struct{}{}
	for {
		select {
		case c := <-ch:
			fmt.Printf("signal received: %v", c)
			return
		case <-ticker.C:
		case <-first:
		}

		eventss := make([][]*Event, len(cfg.CalendarEndpoints))
		for i, ep := range cfg.CalendarEndpoints {
			events, err := loadCalendar(ep)

			if err != nil {
				log.Printf("failed to load config: %v", err)

				continue
			}

			eventss[i] = events
		}

		events := uniqueEvents(eventss...)

		discordEvents := make([]*DiscordEvent, 0, 32)
		err = db.Update(func(tx *bolt.Tx) error {
			bucket, _ := tx.CreateBucketIfNotExists([]byte("events"))
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
				} else if event.LastModified.After(old.Event.LastModified.Add(10 * time.Second)) {
					de = &DiscordEvent{
						Kind:  "UPDATE",
						Event: event,
					}
				} else {
					for _, n := range cfg.Notification.Schedules {
						ns := time.Duration(n)
						sub := event.End.Sub(updated.LastChecked)

						if event.End.Sub(old.LastChecked) > ns && sub <= ns && sub >= 0 {
							de = &DiscordEvent{
								Kind:  "NOTIFY",
								Param: ns,
								Event: event,
							}
							break
						}
					}
				}

				if de != nil {
					discordEvents = append(discordEvents, de)
				}
				b, _ = json.Marshal(updated)
				bucket.Put([]byte(event.UID), b)
			}

			return nil
		})

		if err != nil {
			log.Printf("failed to run transaction: %+v", err)
		}

		channels, err := session.GuildChannels(cfg.Discord.GuildID)

		if err != nil {
			log.Printf("failed to list discord channels: %+v", err)
		}
		if len(channels) == 0 {
			continue
		}
		defaultChannelID := channels[0].ID
		for i := range channels {
			if channels[i].Name == cfg.Discord.DefaultChannel {
				defaultChannelID = channels[i].ID
			}
		}

		for _, de := range discordEvents {
			id, _ := mostMatchedChannel(cfg.Discord.Parent, channels, de)

			if id == "" {
				id = defaultChannelID
			}

			tmpl, ok := cfg.Notification.ParsedTemplates[strings.ToLower(de.Kind)]

			encoded, _ := json.Marshal(de)
			var _ *discordgo.Message
			if !ok {
				log.Printf("unknown template for %s: %+v", de.Kind, err)
				_, err = session.ChannelMessageSend(id, string(encoded))
			} else {
				buf := bytes.NewBuffer(nil)

				if err := tmpl.Execute(buf, de); err != nil {
					log.Printf("template execution failed for %s: %+v", de.Kind, err)
					_, err = session.ChannelMessageSend(id, string(encoded))
				} else {
					_, err = session.ChannelMessageSend(id, buf.String())
				}
			}

			if err != nil {
				log.Printf("failed to send message: %+v", err)
			}
		}

	}
}
