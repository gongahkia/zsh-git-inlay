package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/gongahkia/zsh-git-inlay/internal/evaluation"
)

func readReviewJSON(path string, value any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 8*1024*1024+1))
	if err != nil {
		return err
	}
	if len(content) > 8*1024*1024 {
		return errors.New("review input exceeds 8 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("trailing review input")
	}
	return nil
}

func reviewCommand(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("usage: zsh-git-inlay review {prepare|summarize}")
	}
	flags := flag.NewFlagSet("review", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	left := flags.String("left", "", "left private evaluation JSON report")
	right := flags.String("right", "", "right private evaluation JSON report")
	output := flags.String("output-dir", "", "private packet directory")
	packetPath := flags.String("packet", "", "blinded packet JSON")
	keyPath := flags.String("key", "", "private key JSON")
	ratingsPath := flags.String("ratings", "", "explicit human ratings JSON")
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected review arguments")
	}
	switch arguments[0] {
	case "prepare":
		if *left == "" || *right == "" || *output == "" {
			return errors.New("review prepare requires --left --right --output-dir")
		}
		var a, b evaluation.Report
		if err := readReviewJSON(*left, &a); err != nil {
			return err
		}
		if err := readReviewJSON(*right, &b); err != nil {
			return err
		}
		packet, key, err := evaluation.PrepareReview(a, b)
		if err != nil {
			return err
		}
		if err := evaluation.WriteReview(*output, packet, key); err != nil {
			return err
		}
		fmt.Println("Private review packet prepared. Give packet.json to the reviewer and keep key.json separate. Write actual measurements to a new ratings.json; ratings.example.json is a schema example, not collected evidence.")
		return nil
	case "summarize":
		if *packetPath == "" || *keyPath == "" || *ratingsPath == "" {
			return errors.New("review summarize requires --packet --key --ratings")
		}
		var packet evaluation.ReviewPacket
		var key evaluation.ReviewKey
		var ratings []evaluation.HumanRating
		for _, item := range []struct {
			path  string
			value any
		}{{*packetPath, &packet}, {*keyPath, &key}, {*ratingsPath, &ratings}} {
			if err := readReviewJSON(item.path, item.value); err != nil {
				return err
			}
		}
		summary, err := evaluation.SummarizeReview(packet, key, ratings)
		if err != nil {
			return err
		}
		return printJSON(summary)
	default:
		return errors.New("review operation must be prepare or summarize")
	}
}
