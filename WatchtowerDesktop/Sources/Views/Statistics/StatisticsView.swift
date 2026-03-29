import SwiftUI

struct StatisticsView: View {
    @State private var selectedTab = 0

    var body: some View {
        TabView(selection: $selectedTab) {
            ChannelStatisticsView()
                .tabItem {
                    Label("Channels", systemImage: "number")
                }
                .tag(0)
            UserStatisticsView()
                .tabItem {
                    Label("Users", systemImage: "person.2")
                }
                .tag(1)
            ActivityStatisticsView()
                .tabItem {
                    Label("Activity", systemImage: "waveform.path.ecg")
                }
                .tag(2)
        }
    }
}
