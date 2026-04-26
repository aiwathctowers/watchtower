package cmd

import "testing"

func TestMeetingExtractTopicsCmdHasFlags(t *testing.T) {
	if meetingExtractTopicsCmd.Parent() != meetingPrepCmd {
		t.Fatalf("meeting-prep extract-topics should be a subcommand of meeting-prep, parent=%v",
			meetingExtractTopicsCmd.Parent())
	}

	textFlag := meetingExtractTopicsCmd.Flags().Lookup("text")
	if textFlag == nil {
		t.Fatal("meeting-prep extract-topics should have --text flag")
	}

	jsonFlag := meetingExtractTopicsCmd.Flags().Lookup("json")
	if jsonFlag == nil {
		t.Fatal("meeting-prep extract-topics should have --json flag")
	}

	eventFlag := meetingExtractTopicsCmd.Flags().Lookup("event-id")
	if eventFlag == nil {
		t.Fatal("meeting-prep extract-topics should have --event-id flag")
	}
}

func TestMeetingExtractTopicsRejectsEmptyText(t *testing.T) {
	meetingExtractTopicsFlagText = ""
	err := runMeetingExtractTopics(meetingExtractTopicsCmd, nil)
	if err == nil {
		t.Fatal("expected error when --text is empty")
	}
}
