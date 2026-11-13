// Command fiber is a sample embedding of the engine supervisor behind
// a small HTTP API — a wiring demonstration, not a product:
//
//	POST /api/scv/analyze  {"source": "...", ...}  -> {"id": "j_..."}
//	GET  /api/scv/jobs/:id                        -> job status JSON
//	GET  /api/scv/jobs/:id/events                 -> SSE event stream
//	GET  /api/scv/env                             -> environment JSON
//
// Run with: go run ./examples/fiber  (SCV_HOME overrides the scv home).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/projanvil/scv/engine/internal/config"
	"github.com/projanvil/scv/engine/internal/job"
	"github.com/projanvil/scv/engine/internal/run"
	"github.com/projanvil/scv/engine/supervisor"
)

func main() {
	home := os.Getenv("SCV_HOME")
	if home == "" {
		user, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		home = filepath.Join(user, ".scv")
	}
	cfg, err := config.Load(filepath.Join(home, "config.json"), home)
	if err != nil {
		log.Fatal(err)
	}
	sup, err := supervisor.New(context.Background(), *cfg)
	if err != nil {
		log.Fatal(err)
	}

	// The sample keeps its own id -> handle map; the supervisor hands
	// out handles only at Run time.
	var (
		mu      sync.Mutex
		handles = map[string]*supervisor.JobHandle{}
	)

	app := fiber.New()

	app.Post("/api/scv/analyze", func(c *fiber.Ctx) error {
		var req run.RunRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		// The request context bounds only the synchronous start phase;
		// the run itself derives from the supervisor's context.
		h, err := sup.Run(c.UserContext(), req)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		mu.Lock()
		handles[h.ID] = h
		mu.Unlock()
		return c.JSON(fiber.Map{"id": h.ID})
	})

	app.Get("/api/scv/jobs/:id", func(c *fiber.Ctx) error {
		mu.Lock()
		h := handles[c.Params("id")]
		mu.Unlock()
		if h == nil {
			return fiber.ErrNotFound
		}
		j, err := h.Status(c.UserContext())
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(j)
	})

	app.Get("/api/scv/jobs/:id/events", func(c *fiber.Ctx) error {
		mu.Lock()
		h := handles[c.Params("id")]
		mu.Unlock()
		if h == nil {
			return fiber.ErrNotFound
		}
		c.Set(fiber.HeaderContentType, "text/event-stream")
		c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
			// The stream outlives the request context, so the poll loop
			// runs on a fresh one; a dropped connection surfaces as a
			// failed Flush/Write and ends the loop.
			since := 0
			for {
				events, err := h.Events(context.Background(), since)
				if err != nil {
					return
				}
				for _, ev := range events {
					line, err := json.Marshal(ev)
					if err != nil {
						return
					}
					fmt.Fprintf(w, "data: %s\n\n", line)
					since = ev.Seq
				}
				if err := w.Flush(); err != nil {
					return
				}
				// The terminal job_result event closes the stream.
				if n := len(events); n > 0 && events[n-1].Type == job.EventJobResult {
					return
				}
				time.Sleep(200 * time.Millisecond)
			}
		})
		return nil
	})

	app.Get("/api/scv/env", func(c *fiber.Ctx) error {
		e, err := sup.Env(c.UserContext())
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(e)
	})

	log.Fatal(app.Listen(":8080"))
}
