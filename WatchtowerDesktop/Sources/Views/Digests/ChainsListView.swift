import SwiftUI

struct ChainsListView: View {
    let viewModel: ChainsViewModel
    @Binding var selectedChainID: Int?
    @Binding var searchText: String

    private var filteredChains: [Chain] {
        if searchText.isEmpty { return viewModel.chains }
        let query = searchText.lowercased()
        return viewModel.chains.filter {
            $0.title.lowercased().contains(query) ||
            $0.summary.lowercased().contains(query) ||
            $0.slug.lowercased().contains(query)
        }
    }

    var body: some View {
        if filteredChains.isEmpty {
            ContentUnavailableView(
                "No Chains",
                systemImage: "link.circle",
                description: Text("Chains are created automatically when related decisions are detected across digests.")
            )
        } else {
            ScrollView {
                LazyVStack(spacing: 8) {
                    ForEach(filteredChains) { chain in
                        chainRow(chain)
                            .contentShape(Rectangle())
                            .onTapGesture { selectedChainID = chain.id }
                            .padding(.horizontal, 12)
                            .padding(.vertical, 8)
                            .background(
                                selectedChainID == chain.id
                                    ? Color.accentColor.opacity(0.12)
                                    : !chain.isRead
                                        ? Color.blue.opacity(0.06)
                                        : Color(nsColor: .controlBackgroundColor),
                                in: RoundedRectangle(cornerRadius: 8)
                            )
                            .overlay(
                                RoundedRectangle(cornerRadius: 8)
                                    .strokeBorder(
                                        selectedChainID == chain.id
                                            ? Color.accentColor.opacity(0.3)
                                            : !chain.isRead
                                                ? Color.blue.opacity(0.25)
                                                : Color.primary.opacity(0.06),
                                        lineWidth: 1
                                    )
                            )
                    }
                }
                .padding(.vertical, 8)
                .padding(.horizontal, 8)
            }
        }
    }

    @ViewBuilder
    private func chainRow(_ chain: Chain) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                statusIcon(chain.status)
                if !chain.isRead {
                    Circle()
                        .fill(.blue)
                        .frame(width: 8, height: 8)
                }
                Text(chain.title)
                    .font(.subheadline)
                    .fontWeight(chain.isRead ? .regular : .medium)
                    .lineLimit(1)
                Spacer()
                Text(chain.lastSeenDate, style: .relative)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }

            if !chain.summary.isEmpty {
                Text(chain.summary)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }

            HStack(spacing: 6) {
                chainStatusBadge(chain.status)

                Text("\(chain.itemCount) items")
                    .font(.system(size: 9, weight: .semibold))
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 5)
                    .padding(.vertical, 1)
                    .background(.quaternary, in: Capsule())

                let channelIDs = chain.decodedChannelIDs
                if !channelIDs.isEmpty {
                    HStack(spacing: 4) {
                        ForEach(Array(channelIDs.prefix(3)), id: \.self) { chID in
                            if let url = viewModel.slackChannelURL(channelID: chID) {
                                Link(destination: url) {
                                    Text("#" + viewModel.channelName(for: chID))
                                        .font(.caption)
                                }
                                .buttonStyle(.borderless)
                            } else {
                                Text("#" + viewModel.channelName(for: chID))
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }
                        if channelIDs.count > 3 {
                            Text("+\(channelIDs.count - 3)")
                                .font(.caption)
                                .foregroundStyle(.tertiary)
                        }
                    }
                }

                Spacer()

                let children = viewModel.children(for: chain.id)
                if !children.isEmpty {
                    HStack(spacing: 4) {
                        Image(systemName: "arrow.triangle.branch")
                            .font(.system(size: 9))
                            .foregroundStyle(.blue)
                        Text("\(children.count)")
                            .font(.caption2)
                            .foregroundStyle(.blue)
                    }
                }
            }
        }
        .padding(.vertical, 4)
    }

    @ViewBuilder
    private func statusIcon(_ status: String) -> some View {
        switch status {
        case "active":
            Image(systemName: "link.circle.fill")
                .foregroundStyle(.blue)
                .font(.caption)
        case "resolved":
            Image(systemName: "checkmark.circle.fill")
                .foregroundStyle(.green)
                .font(.caption)
        case "stale":
            Image(systemName: "moon.zzz.fill")
                .foregroundStyle(.gray)
                .font(.caption)
        default:
            Image(systemName: "link.circle")
                .foregroundStyle(.secondary)
                .font(.caption)
        }
    }

    @ViewBuilder
    private func chainStatusBadge(_ status: String) -> some View {
        let (text, color): (String, Color) = switch status {
        case "active": ("Active", .blue)
        case "resolved": ("Resolved", .green)
        case "stale": ("Stale", .gray)
        default: (status, .secondary)
        }
        Text(text)
            .font(.system(size: 9, weight: .semibold))
            .foregroundStyle(color)
            .padding(.horizontal, 5)
            .padding(.vertical, 1)
            .background(color.opacity(0.12), in: Capsule())
    }
}
