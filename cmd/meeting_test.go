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

func TestMeetingRecapCmdHasRequiredFlags(t *testing.T) {
	if meetingRecapCmd.Flags().Lookup("event-id") == nil {
		t.Error("meeting-prep recap should have --event-id flag")
	}
	if meetingRecapCmd.Flags().Lookup("text") == nil {
		t.Error("meeting-prep recap should have --text flag")
	}
}

func TestMeetingRecapCmdRequiresEventID(t *testing.T) {
	// Reset flags to default and run with only --text
	meetingRecapFlagText = "some text"
	meetingRecapFlagEventID = ""
	err := runMeetingRecap(meetingRecapCmd, nil)
	if err == nil {
		t.Fatal("expected error when --event-id is missing")
	}
}

func TestMeetingRecapCmdRequiresText(t *testing.T) {
	meetingRecapFlagText = ""
	meetingRecapFlagEventID = "evt-1"
	err := runMeetingRecap(meetingRecapCmd, nil)
	if err == nil {
		t.Fatal("expected error when --text is missing")
	}
}
