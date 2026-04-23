import SwiftUI

struct DayPlanView: View {
    @Bindable var vm: DayPlanViewModel
    @State private var showRegen = false
    @State private var showCreate = false

    var body: some View {
        mainContent(vm)
            .task { await vm.loadFor(date: todayString()) }
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

                    if vm.timeblocks.isEmpty {
                        Text("No scheduled timeblocks")
                            .font(.callout)
                            .foregroundStyle(.tertiary)
                            .padding(.horizontal, 16)
                            .padding(.vertical, 12)
                    } else {
                        DayPlanTimelineView(items: vm.timeblocks) { item in
                            Task {
                                if item.isDone {
                                    await vm.markPending(item)
                                } else {
                                    await vm.markDone(item)
                                }
                            }
                        }
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
        .task {
            await vm.loadFor(date: todayString())
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

            let (done, total) = vm.progress
            if total > 0 {
                Text("\(done)/\(total)")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
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

    // MARK: - Helpers

    private func todayString() -> String {
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        return fmt.string(from: Date())
    }
}
