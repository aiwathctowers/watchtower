import SwiftUI

struct DayPlanView: View {
    @Bindable var vm: DayPlanViewModel
    @Environment(AppState.self) private var appState
    @State private var showRegen = false
    @State private var showCreate = false
    @State private var meetingPrepEventID: String?
    @State private var meetingPrepVM = MeetingPrepViewModel()
    @State private var userNotes: String = ""
    @State private var briefingExistsForDate: Bool = false

    var body: some View {
        mainContent(vm)
    }

    // MARK: - Main Content

    @ViewBuilder
    private func mainContent(_ vm: DayPlanViewModel) -> some View {
        VStack(spacing: 0) {
            headerBar(vm)
            Divider()

            if vm.hasConflicts {
                DayPlanConflictBanner(
                    summary: vm.plan?.conflictSummary,
                    onRegenerate: { showRegen = true },
                    onCheckAgain: { Task { await vm.checkConflicts() } }
                )
                .padding(.top, 8)
            }

            if let errorMsg = vm.generationError {
                HStack(spacing: 6) {
                    Image(systemName: "xmark.circle.fill")
                        .foregroundStyle(.red)
                    Text(errorMsg)
                        .font(.caption)
                        .foregroundStyle(.red)
                    Spacer()
                    Button("Dismiss") { vm.generationError = nil }
                        .font(.caption)
                        .buttonStyle(.plain)
                        .foregroundStyle(.secondary)
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 6)
            }

            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    // Timeline section
                    sectionHeader("TIMELINE")

                    if vm.timeblocks.isEmpty && vm.allDayItems.isEmpty {
                        Text("No scheduled timeblocks")
                            .font(.callout)
                            .foregroundStyle(.tertiary)
                            .padding(.horizontal, 16)
                            .padding(.vertical, 12)
                    } else {
                        DayPlanTimelineView(
                            items: vm.timeblocks,
                            allDayEvents: vm.allDayItems,
                            calendarEventsByID: vm.calendarEventsByID,
                            onToggle: { item in
                                Task {
                                    if item.isDone {
                                        await vm.markPending(item)
                                    } else {
                                        await vm.markDone(item)
                                    }
                                }
                            },
                            onPrepare: { eventID in
                                // Fresh VM per meeting avoids showing cached prep from a previous event.
                                meetingPrepVM = MeetingPrepViewModel()
                                meetingPrepEventID = eventID
                                meetingPrepVM.generate(eventID: eventID)
                            },
                            onNavigate: { item in
                                navigateToSource(item)
                            }
                        )
                    }

                    // Backlog section
                    HStack {
                        sectionHeader("BACKLOG (if time permits)")
                        Spacer()
                        Button {
                            showCreate = true
                        } label: {
                            Label("Add", systemImage: "plus")
                                .font(.caption)
                        }
                        .buttonStyle(.borderless)
                        .padding(.trailing, 16)
                    }

                    if vm.backlogItems.isEmpty {
                        Text("No backlog items")
                            .font(.callout)
                            .foregroundStyle(.tertiary)
                            .padding(.horizontal, 16)
                            .padding(.vertical, 12)
                    } else {
                        ForEach(vm.backlogItems) { item in
                            DayPlanItemRow(
                                item: item,
                                onToggle: {
                                    Task {
                                        if item.isDone {
                                            await vm.markPending(item)
                                        } else {
                                            await vm.markDone(item)
                                        }
                                    }
                                },
                                onDelete: {
                                    Task { await vm.delete(item) }
                                },
                                onNavigateSource: {
                                    navigateToSource(item)
                                }
                            )
                            Divider()
                                .padding(.leading, 42)
                        }
                    }
                }
                .padding(.bottom, 16)
            }

            Divider()
            footerBar(vm)
        }
        .sheet(isPresented: $showRegen) {
            RegenerateFeedbackSheet(vm: vm, isPresented: $showRegen)
        }
        .sheet(isPresented: $showCreate) {
            CreateDayPlanItemSheet(vm: vm, isPresented: $showCreate)
        }
        .sheet(isPresented: Binding(
            get: { meetingPrepEventID != nil },
            set: { if !$0 { meetingPrepEventID = nil } }
        )) {
            if let id = meetingPrepEventID {
                MeetingPrepDetailView(
                    eventID: id,
                    viewModel: meetingPrepVM,
                    userNotes: $userNotes,
                    onClose: { meetingPrepEventID = nil }
                )
                .id(id)
                .frame(minWidth: 480, minHeight: 520)
            }
        }
        .task {
            let date = appState.pendingDayPlanDate ?? todayString()
            await vm.loadFor(date: date)
            if appState.pendingDayPlanDate == date {
                appState.pendingDayPlanDate = nil
            }
            refreshBriefingExists()
        }
        .onChange(of: appState.pendingDayPlanDate) { _, newDate in
            guard let d = newDate else { return }
            Task {
                await vm.loadFor(date: d)
                if appState.pendingDayPlanDate == d {
                    appState.pendingDayPlanDate = nil
                }
                refreshBriefingExists()
            }
        }
        .onChange(of: vm.plan?.planDate) { _, _ in
            refreshBriefingExists()
        }
    }

    // MARK: - Header

    private func headerBar(_ vm: DayPlanViewModel) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text("Day Plan — \(vm.plan?.planDate ?? todayString())")
                    .font(.title2)
                    .fontWeight(.bold)
            }

            Spacer()

            if briefingExistsForDate || vm.plan?.briefingId != nil {
                Button {
                    openBriefing()
                } label: {
                    Label("Briefing", systemImage: "sun.max")
                        .font(.caption)
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
                .help("Open today's briefing")
            }

            let (done, total) = vm.progress
            if total > 0 {
                Text("\(done)/\(total)")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
                    .padding(.leading, 8)
            }

            if vm.isGenerating {
                ProgressView()
                    .controlSize(.small)
                    .padding(.leading, 4)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
    }

    // MARK: - Section Header

    private func sectionHeader(_ title: String) -> some View {
        Text(title)
            .font(.caption)
            .fontWeight(.semibold)
            .foregroundStyle(.secondary)
            .padding(.horizontal, 16)
            .padding(.top, 16)
            .padding(.bottom, 6)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    // MARK: - Footer

    private func footerBar(_ vm: DayPlanViewModel) -> some View {
        HStack(spacing: 12) {
            if vm.plan == nil {
                Button("Generate today's plan") {
                    Task { await vm.regenerate(feedback: nil) }
                }
                .buttonStyle(.borderedProminent)
                .disabled(vm.isGenerating)
            } else {
                Button("Regenerate with feedback…") {
                    showRegen = true
                }
                .buttonStyle(.bordered)
                .disabled(vm.isGenerating)
            }

            Spacer()

            if vm.plan != nil {
                Button("Reset plan") {
                    Task { await vm.reset() }
                }
                .buttonStyle(.bordered)
                .foregroundStyle(.red)
                .disabled(vm.isGenerating)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
    }

    // MARK: - Navigation

    private func navigateToSource(_ item: DayPlanItem) {
        guard let sid = item.sourceId, !sid.isEmpty else { return }
        switch item.sourceType {
        case .task:
            if let id = Int(sid) {
                appState.navigateToTask(id)
            }
        case .briefingAttention:
            // briefing_attention source_id points at the attention item's inner source
            // (track/digest/people/task), not a briefings.id. Route to the plan's linked
            // briefing, or fall back to the briefings list.
            if let bid = vm.plan?.briefingId {
                appState.navigateToBriefing(Int(bid))
            } else {
                appState.selectedDestination = .briefings
            }
        case .jira:
            appState.selectedDestination = .boards
        case .focus, .calendar, .manual:
            break
        }
    }

    private func openBriefing() {
        if let bid = vm.plan?.briefingId {
            appState.navigateToBriefing(Int(bid))
        } else {
            appState.selectedDestination = .briefings
        }
    }

    private func refreshBriefingExists() {
        guard let db = appState.databaseManager else {
            briefingExistsForDate = false
            return
        }
        let dateStr = vm.plan?.planDate ?? todayString()
        briefingExistsForDate = (try? db.dbPool.read { db in
            try BriefingQueries.fetchByDate(db, date: dateStr) != nil
        }) ?? false
    }

    // MARK: - Helpers

    private func todayString() -> String {
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        return fmt.string(from: Date())
    }
}
