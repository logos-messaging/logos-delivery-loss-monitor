package api

import (
	"net/http"
	"net/url"
	"time"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/apierr"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/auth"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/stats"
)

type statsCounts struct {
	Expected              int64            `json:"expected"`
	Delivered             int64            `json:"delivered"`
	DeliveredLive         int64            `json:"delivered_live"`
	DeliveredHistory      int64            `json:"delivered_history"`
	Lost                  int64            `json:"lost"`
	Late                  int64            `json:"late"`
	Duplicate             int64            `json:"duplicate"`
	Replayed              int64            `json:"replayed"`
	Indeterminate         int64            `json:"indeterminate"`
	IndeterminateByReason map[string]int64 `json:"indeterminate_by_reason"`
	LossRatio             *float64         `json:"loss_ratio"`
}

type pairRow struct {
	Topic             string `json:"content_topic"`
	PublisherMode     string `json:"publisher_mode"`
	ReceiverMode      string `json:"receiver_mode"`
	PublisherLocation string `json:"publisher_location"`
	ReceiverLocation  string `json:"receiver_location"`
	PublisherAnon     string `json:"publisher_anonymity"`
	statsCounts
}

type clientRow struct {
	Topic          string `json:"content_topic"`
	PublisherToken string `json:"publisher_token"`
	PublisherID    string `json:"publisher_client_id"`
	ReceiverToken  string `json:"receiver_token"`
	ReceiverID     string `json:"receiver_client_id"`
	statsCounts
}

type statsResponse struct {
	From       string      `json:"from"`
	To         string      `json:"to"`
	Resolution string      `json:"resolution"`
	Partial    bool        `json:"partial"`
	Totals     statsCounts `json:"totals"`
	By         string      `json:"by,omitempty"`
	Breakdown  any         `json:"breakdown,omitempty"`
}

func toCounts(c *stats.Counts) statsCounts {
	out := statsCounts{
		Expected: c.Expected, Delivered: c.Delivered(), DeliveredLive: c.DeliveredLive, DeliveredHistory: c.DeliveredHistory,
		Lost: c.Lost, Late: c.Late, Duplicate: c.Duplicate, Replayed: c.Replayed, Indeterminate: c.IndeterminateTotal(),
		IndeterminateByReason: make(map[string]int64, len(stats.Reasons)),
	}
	for i, r := range stats.Reasons {
		out.IndeterminateByReason[r] = c.Indeterminate[i]
	}
	if ratio, ok := c.LossRatio(); ok {
		out.LossRatio = &ratio
	}
	return out
}

func (s *Server) parseStatsQuery(v url.Values) (stats.Query, *apierr.Error) {
	bad := func(detail string) (stats.Query, *apierr.Error) {
		return stats.Query{}, apierr.New(http.StatusUnprocessableEntity, apierr.InvalidRequest, detail)
	}
	from, err := time.Parse(time.RFC3339, v.Get("from"))
	if err != nil {
		return bad("from must be an RFC 3339 time")
	}
	to, err := time.Parse(time.RFC3339, v.Get("to"))
	if err != nil {
		return bad("to must be an RFC 3339 time")
	}
	if !to.After(from) {
		return bad("to must be after from")
	}
	q := stats.Query{From: from.UnixMilli(), To: to.UnixMilli(), Topic: v.Get("topic"), By: v.Get("by")}
	if q.Topic != "" {
		found := false
		for _, t := range s.cfg.Topics {
			found = found || t == q.Topic
		}
		if !found {
			return bad("topic is not in the allowlist")
		}
	}
	if q.By != "" && q.By != stats.ByPair && q.By != stats.ByClient {
		return bad("by must be pair or client")
	}
	return q, nil
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request, who auth.Identity) {
	q, e := s.parseStatsQuery(r.URL.Query())
	if e != nil {
		s.fail(w, e)
		return
	}
	res, e := s.engine.Stats(r.Context(), q)
	if e != nil {
		s.fail(w, e)
		return
	}
	if q.By == stats.ByClient {
		s.log.Info("stats query", "token", who.Name, "by", q.By)
	}
	out := statsResponse{
		From:       time.UnixMilli(res.From).UTC().Format(time.RFC3339),
		To:         time.UnixMilli(res.To).UTC().Format(time.RFC3339),
		Resolution: "10m", Partial: res.Partial, Totals: toCounts(&res.Totals), By: q.By,
	}
	switch q.By {
	case stats.ByPair:
		rows := make([]pairRow, 0, len(res.Pairs))
		for _, p := range res.Pairs {
			k := p.Key
			rows = append(rows, pairRow{k.Topic, k.PubMode, k.RecvMode, k.PubLoc, k.RecvLoc, k.PubAnon, toCounts(&p.Counts)})
		}
		out.Breakdown = rows
	case stats.ByClient:
		rows := make([]clientRow, 0, len(res.Clients))
		for _, c := range res.Clients {
			k := c.Key
			rows = append(rows, clientRow{k.Topic, k.PubToken, k.PubClient, k.RecvToken, k.RecvClient, toCounts(&c.Counts)})
		}
		out.Breakdown = rows
	}
	writeJSON(w, http.StatusOK, out)
}
