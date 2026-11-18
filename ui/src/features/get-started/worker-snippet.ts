export const workerProgram = `package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/runcoil/runcoil/sdks/go/pkg/runcoil"
	"github.com/runcoil/runcoil/sdks/go/pkg/step"
)

func sayHello(ctx context.Context, _ struct{}) (string, error) {
	return step.Do(ctx, "greet", func(ctx context.Context) (string, error) {
		return "Hello from Runcoil", nil
	})
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client, err := runcoil.NewClient() // reads RUNCOIL_URL and RUNCOIL_API_KEY
	if err != nil {
		log.Fatal(err)
	}

	worker, err := runcoil.NewWorker(client, "hello")
	if err != nil {
		log.Fatal(err)
	}

	// The schedule starts a Run every minute.
	err = worker.NewTask("say-hello", runcoil.HandlerFunc(sayHello), runcoil.Cron("* * * * *"))
	if err != nil {
		log.Fatal(err)
	}

	if err := worker.Start(ctx); err != nil {
		log.Fatal(err)
	}

	<-ctx.Done()

	drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := worker.Shutdown(drainCtx); err != nil {
		log.Print(err)
	}
}
`;

// The server's worker port is 7070 unless it was changed, on the host that serves the dashboard.
export function workerCommands(location: { protocol: string; hostname: string }): string {
  return [
    "go mod init hello",
    "go mod tidy",
    `RUNCOIL_URL=${location.protocol}//${location.hostname}:7070 RUNCOIL_API_KEY=<your API key> go run .`,
  ].join("\n");
}
