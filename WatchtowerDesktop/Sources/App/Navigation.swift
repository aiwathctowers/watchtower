import SwiftUI

struct NavigationRoot: View {
    @Environment(AppState.self) private var appState

    var body: some View {
        if appState.isLoading {
            SplashView()
        } else if appState.needsOnboarding {
            OnboardingView {
                appState.initialize()
            }
        } else {
            MainNavigationView()
        }
    }
}

struct SplashView: View {
    @State private var opacity: Double = 0

    var body: some View {
        VStack(spacing: 24) {
            Spacer()

            BannerImage(maxWidth: 360)

            ProgressView()
                .scaleEffect(0.8)
                .padding(.top, 8)

            Spacer()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(nsColor: .windowBackgroundColor))
        .opacity(opacity)
        .onAppear {
            withAnimation(.easeIn(duration: 0.4)) {
                opacity = 1
            }
        }
    }
}

struct MainNavigationView: View {
    @Environment(AppState.self) private var appState
    @State private var showMenu = true
    @State private var googleAuth = GoogleAuthService()
    @State private var dismissedAuthTimestamp: String = UserDefaults.standard.string(forKey: "dismissedCalendarAuthAt") ?? ""

    /// Show the reconnect popup when the daemon has flagged the calendar auth as broken
    /// AND the user hasn't already dismissed this specific revocation.
    private var shouldShowReconnectAlert: Bool {
        guard let auth = appState.calendarViewModel?.authState else { return false }
        guard auth.status == "revoked" else { return false }
        return auth.updatedAt != dismissedAuthTimestamp
    }

    private var sidebarToggleRow: some View {
        HStack(spacing: 8) {
            Button {
                withAnimation(.easeInOut(duration: 0.2)) {
                    showMenu.toggle()
                }
            } label: {
                Image(systemName: "sidebar.leading")
                    .foregroundStyle(.secondary)
            }
            .buttonStyle(.borderless)
            .help("Toggle Menu")
            .keyboardShortcut("b", modifiers: [.command])

            Spacer()
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
    }

    var body: some View {
        @Bindable var state = appState
        VStack(spacing: 0) {
            // Content
            HStack(spacing: 0) {
                if showMenu {
                    // Left sidebar with toggle button in toolbar row
                    VStack(spacing: 0) {
                        sidebarToggleRow

                        SidebarView(selection: $state.selectedDestination)
                    }
                    .frame(width: 180)
                    .background(Color(nsColor: .windowBackgroundColor))
                    .transition(.move(edge: .leading).combined(with: .opacity))

                    Divider()
                }

                // Main content
                VStack(spacing: 0) {
                    if !showMenu {
                        sidebarToggleRow
                            .background(Color(nsColor: .windowBackgroundColor))
                    }

                    detailView
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                        .background(Color(nsColor: .controlBackgroundColor))
                }
            }

            StatusBarView()
        }
        .background(Color(nsColor: .windowBackgroundColor))
        .alert(
            "Google Calendar disconnected",
            isPresented: Binding(
                get: { shouldShowReconnectAlert },
                set: { newValue in
                    if !newValue, let auth = appState.calendarViewModel?.authState {
                        dismissedAuthTimestamp = auth.updatedAt
                        UserDefaults.standard.set(auth.updatedAt, forKey: "dismissedCalendarAuthAt")
                    }
                }
            )
        ) {
            Button("Reconnect") {
                appState.selectedDestination = .calendar
                reconnectAndRestartDaemon()
            }
            Button("Later", role: .cancel) {}
        } message: {
            Text("Your Google authorization expired or was revoked. Reconnect to resume calendar sync.")
        }
    }

    /// Runs the OAuth flow and, on success, restarts the daemon so the in-memory
    /// refresh token is replaced with the freshly saved one.
    private func reconnectAndRestartDaemon() {
        googleAuth.connect()
        Task {
            while googleAuth.isAuthenticating {
                try? await Task.sleep(for: .milliseconds(250))
            }
            guard googleAuth.isConnected else { return }
            let daemon = DaemonManager()
            daemon.resolvePathIfNeeded()
            guard DaemonManager.checkDaemonRunning() else { return }
            await daemon.stopDaemon()
            try? await Task.sleep(for: .milliseconds(500))
            await daemon.startDaemon()
        }
    }

    @ViewBuilder
    private var detailView: some View {
        switch appState.selectedDestination {
        case .chat:
            ChatView()
        case .briefings:
            BriefingsListView()
        case .dayPlan:
            if let vm = appState.dayPlanViewModel {
                DayPlanView(vm: vm)
            } else {
                Text("Day Plan unavailable")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        case .inbox:
            InboxFeedView()
        case .calendar:
            CalendarEventsView()
        case .targets:
            TargetsListView()
        case .tracks:
            TracksListView()
        case .digests:
            DigestListView()
        case .people:
            PeopleListView()
        case .workload:
            WorkloadView()
        case .blockers:
            BlockerMapView()
        case .projectMap:
            ProjectMapView()
        case .releases:
            ReleaseDashboardView()
        case .statistics:
            StatisticsView()
        case .search:
            SearchView()
        case .boards:
            BoardsView()
        case .usage:
            UsageView()
        case .training:
            TrainingView()
        }
    }
}

// MARK: - Onboarding


