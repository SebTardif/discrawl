package syncer

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/require"

	"github.com/openclaw/discrawl/internal/store"
)

type repeatingMessagePageClient struct {
	fakeClient
	page     []*discordgo.Message
	requests int
}

func (c *repeatingMessagePageClient) ChannelMessages(_ context.Context, _ string, _ int, _, _ string) ([]*discordgo.Message, error) {
	c.requests++
	if c.requests > 5 {
		return nil, fmt.Errorf("message pager called %d times without stopping", c.requests)
	}
	return c.page, nil
}

func fullMessagePage(lastID string) []*discordgo.Message {
	page := make([]*discordgo.Message, 100)
	now := time.Now().UTC()
	author := &discordgo.User{ID: "u1", Username: "user"}
	for i := range 99 {
		page[i] = &discordgo.Message{
			ID:        strconv.Itoa(i + 1),
			GuildID:   "g1",
			ChannelID: "c1",
			Content:   "msg",
			Timestamp: now,
			Author:    author,
		}
	}
	page[99] = &discordgo.Message{
		ID:        lastID,
		GuildID:   "g1",
		ChannelID: "c1",
		Content:   "tail",
		Timestamp: now,
		Author:    author,
	}
	return page
}

func emptyIDMessagePage() []*discordgo.Message {
	page := make([]*discordgo.Message, 100)
	now := time.Now().UTC()
	author := &discordgo.User{ID: "u1", Username: "user"}
	for i := range page {
		page[i] = &discordgo.Message{
			ID:        "",
			GuildID:   "g1",
			ChannelID: "c1",
			Content:   "msg",
			Timestamp: now,
			Author:    author,
		}
	}
	return page
}

func TestMessagePagesErrorWhenCursorDoesNotAdvance(t *testing.T) {
	t.Parallel()

	channel := &discordgo.Channel{ID: "c1", GuildID: "g1", Name: "general", Type: discordgo.ChannelTypeGuildText}

	tests := []struct {
		name      string
		page      []*discordgo.Message
		run       func(*Syncer, *discordgo.Channel) error
		wantErr   string
		wantCalls int
	}{
		{
			name: "bootstrap repeats last id",
			page: fullMessagePage("100"),
			run: func(svc *Syncer, channel *discordgo.Channel) error {
				_, err := svc.bootstrapChannelHistory(context.Background(), channel, false, time.Time{}, nil)
				return err
			},
			wantErr:   "message page cursor did not advance",
			wantCalls: 2,
		},
		{
			name: "bootstrap empty last id",
			page: fullMessagePage(""),
			run: func(svc *Syncer, channel *discordgo.Channel) error {
				_, err := svc.bootstrapChannelHistory(context.Background(), channel, false, time.Time{}, nil)
				return err
			},
			wantErr:   "message page missing id",
			wantCalls: 1,
		},
		{
			name: "forward repeats newest id",
			page: fullMessagePage("100"),
			run: func(svc *Syncer, channel *discordgo.Channel) error {
				_, _, err := svc.syncForwardPages(context.Background(), channel, "50", false, nil)
				return err
			},
			wantErr:   "message page cursor did not advance",
			wantCalls: 2,
		},
		{
			name: "forward empty newest id",
			page: emptyIDMessagePage(),
			run: func(svc *Syncer, channel *discordgo.Channel) error {
				_, _, err := svc.syncForwardPages(context.Background(), channel, "100", false, nil)
				return err
			},
			wantErr:   "message page cursor did not advance",
			wantCalls: 1,
		},
		{
			name: "unlimited backfill repeats last id",
			page: fullMessagePage("100"),
			run: func(svc *Syncer, channel *discordgo.Channel) error {
				_, _, err := svc.syncBackfillPages(context.Background(), channel, "", "", channel.Name, false, time.Time{}, 0, nil)
				return err
			},
			wantErr:   "message page cursor did not advance",
			wantCalls: 2,
		},
		{
			name: "unlimited backfill empty last id",
			page: fullMessagePage(""),
			run: func(svc *Syncer, channel *discordgo.Channel) error {
				_, _, err := svc.syncBackfillPages(context.Background(), channel, "", "", channel.Name, false, time.Time{}, 0, nil)
				return err
			},
			wantErr:   "message page missing id",
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			s, err := store.Open(ctx, filepath.Join(t.TempDir(), "discrawl.db"))
			require.NoError(t, err)
			t.Cleanup(func() { _ = s.Close() })

			client := &repeatingMessagePageClient{page: tt.page}
			svc := New(client, s, nil)
			err = tt.run(svc, channel)
			require.ErrorContains(t, err, tt.wantErr)
			require.Equal(t, tt.wantCalls, client.requests)
		})
	}
}
