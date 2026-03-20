import SwiftUI

struct ConnectionsView: View {
    let interactions: [UserInteraction]
    let profile: UserProfile?
    let allCards: [PeopleCard]
    let userNameResolver: (String) -> String
    var onNavigateToPerson: ((String) -> Void)? = nil
    var onUpdateConnections: (([String], [String], String) -> Void)? = nil

    @State private var selectedNode: String?
    @State private var showEditSheet = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                graphSection
                cardsSection
                editButton
            }
            .padding(20)
        }
    }

    // MARK: - Categorized Connections

    private var orgReportIDs: [String] { profile?.decodedReports ?? [] }
    private var orgPeerIDs: [String] { profile?.decodedPeers ?? [] }
    private var orgManagerID: String { profile?.manager ?? "" }

    private var orgIDs: Set<String> {
        var ids = Set(orgReportIDs + orgPeerIDs)
        if !orgManagerID.isEmpty { ids.insert(orgManagerID) }
        return ids
    }

    /// Interactions with org connections.
    private var orgInteractions: [String: UserInteraction] {
        var map: [String: UserInteraction] = [:]
        for i in interactions where orgIDs.contains(i.userB) {
            map[i.userB] = i
        }
        return map
    }

    /// Discovered connections: NOT in org chart, sorted by score.
    private var discoveredConnections: [UserInteraction] {
        interactions
            .filter { !orgIDs.contains($0.userB) && $0.interactionScore > 0 }
            .sorted { $0.interactionScore > $1.interactionScore }
            .prefix(10)
            .map { $0 }
    }

    /// Discovered connections grouped by type.
    private var discoveredPeers: [UserInteraction] {
        discoveredConnections.filter { $0.connectionType == "peer" }
    }
    private var iDependOn: [UserInteraction] {
        discoveredConnections.filter { $0.connectionType == "i_depend" }
    }
    private var dependOnMe: [UserInteraction] {
        discoveredConnections.filter { $0.connectionType == "depends_on_me" }
    }
    private var weakSignals: [UserInteraction] {
        discoveredConnections.filter { $0.connectionType == "weak" }
    }

    /// All nodes for the graph.
    private var graphNodes: [GraphNode] {
        var nodes: [GraphNode] = []

        if !orgManagerID.isEmpty {
            nodes.append(GraphNode(
                userID: orgManagerID,
                role: .manager,
                interaction: orgInteractions[orgManagerID],
                isOrgConnection: true
            ))
        }
        for id in orgPeerIDs {
            nodes.append(GraphNode(
                userID: id, role: .orgPeer,
                interaction: orgInteractions[id],
                isOrgConnection: true
            ))
        }
        for id in orgReportIDs {
            nodes.append(GraphNode(
                userID: id, role: .report,
                interaction: orgInteractions[id],
                isOrgConnection: true
            ))
        }
        for i in discoveredConnections {
            let role: ConnectionRole = switch i.connectionType {
            case "peer": .discoveredPeer
            case "i_depend": .iDependOn
            case "depends_on_me": .dependsOnMe
            default: .weakSignal
            }
            nodes.append(GraphNode(
                userID: i.userB, role: role,
                interaction: i,
                isOrgConnection: false
            ))
        }
        return Array(nodes.prefix(18))
    }

    private var maxScore: Double {
        max(interactions.map(\.interactionScore).max() ?? 1, 1)
    }

    private func cardFor(_ userID: String) -> PeopleCard? {
        allCards.first { $0.userID == userID }
    }

    // MARK: - Score-based Concentric Graph

    private var graphSection: some View {
        GroupBox("Interaction Graph") {
            ZStack {
                Canvas { context, size in
                    drawGraph(context: context, size: size)
                }
                .frame(height: 460)

                GeometryReader { geo in
                    let center = CGPoint(x: geo.size.width / 2, y: geo.size.height / 2)
                    let maxR = min(geo.size.width, geo.size.height) * 0.40

                    ForEach(graphNodes, id: \.userID) { node in
                        let pos = resolvedPosition(node: node, center: center, maxRadius: maxR)
                        nodeOverlay(node: node, at: pos)
                    }
                }
                .frame(height: 460)
            }

            // Legend
            HStack(spacing: 12) {
                legendDot(color: .blue, label: "Manager")
                legendDot(color: .orange, label: "Org Peer")
                legendDot(color: .green, label: "Report")
                legendDot(color: .cyan, label: "Discovered Peer")
                legendDot(color: .purple, label: "I depend on")
                legendDot(color: .pink, label: "Depends on me")
            }
            .font(.caption2)
            .padding(.top, 4)
        }
    }

    private func legendDot(color: Color, label: String) -> some View {
        HStack(spacing: 3) {
            Circle().fill(color).frame(width: 8, height: 8)
            Text(label).foregroundStyle(.secondary)
        }
    }

    private func drawGraph(context: GraphicsContext, size: CGSize) {
        let center = CGPoint(x: size.width / 2, y: size.height / 2)
        let maxR = min(size.width, size.height) * 0.40

        // Draw concentric rings (score zones)
        for ring in stride(from: 0.33, through: 1.0, by: 0.33) {
            let r = maxR * ring
            let rect = CGRect(x: center.x - r, y: center.y - r, width: r * 2, height: r * 2)
            context.stroke(Circle().path(in: rect),
                          with: .color(.secondary.opacity(0.08)),
                          lineWidth: 1)
        }

        // Draw "ME" node
        let meR: CGFloat = 20
        let meRect = CGRect(x: center.x - meR, y: center.y - meR, width: meR * 2, height: meR * 2)
        context.fill(Circle().path(in: meRect), with: .color(.accentColor.opacity(0.8)))
        context.draw(Text("ME").font(.caption).fontWeight(.bold).foregroundStyle(.white), at: center)

        // Resolve positions with collision avoidance
        let positions = resolvedPositions(center: center, maxRadius: maxR)

        // Draw edges and nodes
        for node in graphNodes {
            guard let pos = positions[node.userID] else { continue }
            let score = node.interaction?.interactionScore ?? 0
            let color = colorForRole(node.role)

            // Edge
            let lineWidth = max(1, CGFloat(score / maxScore) * 3)
            var path = Path()
            path.move(to: center)
            path.addLine(to: pos)

            if node.isOrgConnection {
                context.stroke(path, with: .color(color.opacity(0.35)), lineWidth: lineWidth)
            } else {
                context.stroke(path, with: .color(color.opacity(0.25)),
                              style: StrokeStyle(lineWidth: lineWidth, dash: [5, 3]))
            }

            // Node circle — size by score
            let nodeR = max(7, CGFloat(score / maxScore) * 16)
            let nodeRect = CGRect(x: pos.x - nodeR, y: pos.y - nodeR, width: nodeR * 2, height: nodeR * 2)
            context.fill(Circle().path(in: nodeRect), with: .color(color.opacity(0.6)))
            context.stroke(Circle().path(in: nodeRect), with: .color(color), lineWidth: 1.5)

            // Name label — offset to avoid overlap with node
            let name = userNameResolver(node.userID)
            let shortName = name.count > 12 ? String(name.prefix(12)) + ".." : name
            // Place label outward from center to reduce overlap
            let dx = pos.x - center.x
            let dy = pos.y - center.y
            let dist = sqrt(dx * dx + dy * dy)
            let labelOffset: CGFloat = nodeR + 10
            let labelX = dist > 0 ? pos.x + (dx / dist) * labelOffset : pos.x
            let labelY = dist > 0 ? pos.y + (dy / dist) * labelOffset : pos.y + labelOffset
            context.draw(
                Text(shortName).font(.system(size: 9)).foregroundStyle(.primary),
                at: CGPoint(x: labelX, y: labelY)
            )
        }
    }

    /// Position nodes by score: higher score = closer to center.
    /// Angle determined by role sector.
    private func nodePosition(node: GraphNode, center: CGPoint, maxRadius: CGFloat) -> CGPoint {
        let score = node.interaction?.interactionScore ?? 0
        let normalizedScore = min(score / maxScore, 1.0)
        let radius = maxRadius * (1.0 - normalizedScore * 0.65) // 0.35..1.0 of maxR

        let angle = angleForNode(node)
        return CGPoint(
            x: center.x + CGFloat(Foundation.cos(angle)) * radius,
            y: center.y + CGFloat(Foundation.sin(angle)) * radius
        )
    }

    /// Compute all positions with collision avoidance.
    private func resolvedPositions(center: CGPoint, maxRadius: CGFloat) -> [String: CGPoint] {
        var positions: [String: CGPoint] = [:]
        for node in graphNodes {
            positions[node.userID] = nodePosition(node: node, center: center, maxRadius: maxRadius)
        }

        // Simple repulsion: push overlapping nodes apart
        let minDist: CGFloat = 38
        for _ in 0..<8 {
            let ids = Array(positions.keys)
            for i in 0..<ids.count {
                for j in (i + 1)..<ids.count {
                    guard var p1 = positions[ids[i]], var p2 = positions[ids[j]] else { continue }
                    let dx = p2.x - p1.x
                    let dy = p2.y - p1.y
                    let dist = sqrt(dx * dx + dy * dy)
                    if dist < minDist && dist > 0.01 {
                        let overlap = (minDist - dist) / 2
                        let nx = dx / dist
                        let ny = dy / dist
                        p1.x -= nx * overlap
                        p1.y -= ny * overlap
                        p2.x += nx * overlap
                        p2.y += ny * overlap
                        positions[ids[i]] = p1
                        positions[ids[j]] = p2
                    }
                }
            }
        }
        return positions
    }

    /// Resolved position for overlay hit targets.
    private func resolvedPosition(node: GraphNode, center: CGPoint, maxRadius: CGFloat) -> CGPoint {
        let positions = resolvedPositions(center: center, maxRadius: maxRadius)
        return positions[node.userID] ?? nodePosition(node: node, center: center, maxRadius: maxRadius)
    }

    /// Each role gets a sector of the circle — wider sectors, better distribution.
    private func angleForNode(_ node: GraphNode) -> Double {
        let sameRole = graphNodes.filter { $0.role == node.role }
        let idx = sameRole.firstIndex(where: { $0.userID == node.userID }) ?? 0
        let count = max(sameRole.count, 1)

        // Sectors spread around full circle with gaps between roles.
        // 0 = right, -π/2 = top, π/2 = bottom, ±π = left
        let (sectorStart, sectorEnd): (Double, Double) = switch node.role {
        case .manager:        (-.pi * 0.65, -.pi * 0.35)       // top
        case .orgPeer:        (-.pi * 0.25, .pi * 0.05)        // right-top
        case .report:         (.pi * 0.25, .pi * 0.65)         // bottom-right
        case .discoveredPeer: (.pi * 0.70, .pi * 1.10)         // left-bottom (wide)
        case .iDependOn:      (-.pi * 1.10, -.pi * 0.75)       // left-top
        case .dependsOnMe:    (.pi * 0.08, .pi * 0.22)         // right
        case .weakSignal:     (-.pi * 0.32, -.pi * 0.22)       // gap area
        }

        if count == 1 {
            return (sectorStart + sectorEnd) / 2
        }
        // Ensure minimum angular spacing between nodes within a sector
        let sectorSize = sectorEnd - sectorStart
        let minSpacing = 0.12 // ~7 degrees minimum between nodes
        let neededSpace = minSpacing * Double(count - 1)
        let usedSize = max(sectorSize, neededSpace)
        let startAngle = (sectorStart + sectorEnd) / 2 - usedSize / 2
        let step = usedSize / Double(count)
        return startAngle + step * (Double(idx) + 0.5)
    }

    private func nodeOverlay(node: GraphNode, at position: CGPoint) -> some View {
        Button {
            selectedNode = selectedNode == node.userID ? nil : node.userID
        } label: {
            Color.clear.frame(width: 40, height: 40)
        }
        .buttonStyle(.borderless)
        .position(position)
        .popover(isPresented: Binding(
            get: { selectedNode == node.userID },
            set: { if !$0 { selectedNode = nil } }
        )) {
            nodePopover(node: node)
        }
    }

    private func nodePopover(node: GraphNode) -> some View {
        let card = cardFor(node.userID)
        let name = userNameResolver(node.userID)
        let interaction = node.interaction

        return VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(card?.styleEmoji ?? "")
                Text("@\(name)").fontWeight(.bold)
                Badge(text: node.role.label, color: colorForRole(node.role))
            }

            if let interaction {
                // Score
                HStack(spacing: 8) {
                    Text("Score: \(Int(interaction.interactionScore))")
                        .font(.caption).fontWeight(.medium)
                    Text(interaction.connectionTypeLabel)
                        .font(.caption).foregroundStyle(.secondary)
                }

                // Signal breakdown
                Divider()
                VStack(alignment: .leading, spacing: 3) {
                    if interaction.totalDMs > 0 {
                        signalRow(icon: "envelope", label: "DMs", value: interaction.totalDMs)
                    }
                    if interaction.totalMentions > 0 {
                        signalRow(icon: "at", label: "Mentions", value: interaction.totalMentions)
                    }
                    if interaction.totalThreadReplies > 0 {
                        signalRow(icon: "bubble.left.and.bubble.right", label: "Threads", value: interaction.totalThreadReplies)
                    }
                    if interaction.totalReactions > 0 {
                        signalRow(icon: "hand.thumbsup", label: "Reactions", value: interaction.totalReactions)
                    }
                    if interaction.sharedChannels > 0 {
                        signalRow(icon: "number", label: "Shared channels", value: interaction.sharedChannels)
                    }
                }
            }

            if let card, !card.summary.isEmpty {
                Divider()
                Text(card.summary)
                    .font(.caption).lineLimit(3).foregroundStyle(.secondary)
            }
        }
        .padding(12)
        .frame(width: 260)
    }

    private func signalRow(icon: String, label: String, value: Int) -> some View {
        HStack(spacing: 4) {
            Image(systemName: icon).frame(width: 14)
            Text(label)
            Spacer()
            Text("\(value)").fontWeight(.medium)
        }
        .font(.caption2)
        .foregroundStyle(.secondary)
    }

    // MARK: - Connection Cards

    private var cardsSection: some View {
        VStack(alignment: .leading, spacing: 16) {
            // Org connections
            if !orgManagerID.isEmpty {
                connectionGroup(title: "I Report To", icon: "person.crop.circle",
                               color: .blue, userIDs: [orgManagerID])
            }
            if !orgPeerIDs.isEmpty {
                connectionGroup(title: "Key Peers", icon: "person.2",
                               color: .orange, userIDs: orgPeerIDs)
            }
            if !orgReportIDs.isEmpty {
                connectionGroup(title: "My Reports", icon: "person.3",
                               color: .green, userIDs: orgReportIDs)
            }

            // Discovered connections by type
            if !discoveredPeers.isEmpty {
                connectionGroup(title: "Discovered Peers", icon: "person.2.wave.2",
                               color: .cyan, userIDs: discoveredPeers.map(\.userB))
            }
            if !iDependOn.isEmpty {
                connectionGroup(title: "I Depend On", icon: "arrow.up.right.circle",
                               color: .purple, userIDs: iDependOn.map(\.userB))
            }
            if !dependOnMe.isEmpty {
                connectionGroup(title: "Depend On Me", icon: "arrow.down.left.circle",
                               color: .pink, userIDs: dependOnMe.map(\.userB))
            }
            if !weakSignals.isEmpty {
                connectionGroup(title: "Weak Signals", icon: "antenna.radiowaves.left.and.right",
                               color: .gray, userIDs: weakSignals.map(\.userB))
            }
        }
    }

    private func connectionGroup(title: String, icon: String, color: Color, userIDs: [String]) -> some View {
        GroupBox {
            let columns = [GridItem(.adaptive(minimum: 200, maximum: 280), spacing: 12)]
            LazyVGrid(columns: columns, spacing: 12) {
                ForEach(userIDs, id: \.self) { uid in
                    connectionCard(userID: uid, color: color)
                }
            }
            .padding(4)
        } label: {
            Label(title, systemImage: icon)
                .foregroundStyle(color)
        }
    }

    private func connectionCard(userID: String, color: Color) -> some View {
        let card = cardFor(userID)
        let interaction = interactions.first { $0.userB == userID }
        let name = userNameResolver(userID)

        return VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text(card?.styleEmoji ?? "")
                Text("@\(name)").fontWeight(.medium).lineLimit(1)
                Spacer()
                if let interaction, interaction.interactionScore > 0 {
                    Text("\(Int(interaction.interactionScore))")
                        .font(.caption2).fontWeight(.bold)
                        .padding(.horizontal, 6).padding(.vertical, 2)
                        .background(color.opacity(0.15), in: Capsule())
                }
            }

            if let card {
                HStack(spacing: 6) {
                    Badge(text: card.communicationStyle, color: .accentColor)
                    if card.hasRedFlags {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .foregroundStyle(.red).font(.caption2)
                    }
                }
            }

            if let interaction {
                // Signal badges row
                HStack(spacing: 6) {
                    if interaction.totalDMs > 0 {
                        signalBadge(icon: "envelope", count: interaction.totalDMs)
                    }
                    if interaction.totalMentions > 0 {
                        signalBadge(icon: "at", count: interaction.totalMentions)
                    }
                    if interaction.totalThreadReplies > 0 {
                        signalBadge(icon: "bubble.left.and.bubble.right", count: interaction.totalThreadReplies)
                    }
                    if interaction.totalReactions > 0 {
                        signalBadge(icon: "hand.thumbsup", count: interaction.totalReactions)
                    }
                    if interaction.sharedChannels > 0 {
                        signalBadge(icon: "number", count: interaction.sharedChannels)
                    }
                }

                if !interaction.connectionType.isEmpty && interaction.connectionType != "weak" {
                    Text(interaction.connectionTypeLabel)
                        .font(.caption2).foregroundStyle(.secondary)
                }
            } else {
                Text("No recent interaction")
                    .font(.caption2).foregroundStyle(.tertiary)
            }

            if let summary = card?.summary, !summary.isEmpty {
                Text(summary)
                    .font(.caption).foregroundStyle(.secondary).lineLimit(2)
            }
        }
        .padding(10)
        .background(Color.secondary.opacity(0.05), in: RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .strokeBorder(color.opacity(0.2), lineWidth: 1)
        )
        .onTapGesture {
            onNavigateToPerson?(userID)
        }
    }

    private func signalBadge(icon: String, count: Int) -> some View {
        HStack(spacing: 2) {
            Image(systemName: icon)
            Text("\(count)")
        }
        .font(.caption2)
        .foregroundStyle(.secondary)
        .padding(.horizontal, 4).padding(.vertical, 1)
        .background(Color.secondary.opacity(0.08), in: Capsule())
    }

    // MARK: - Edit Button

    @ViewBuilder
    private var editButton: some View {
        if onUpdateConnections != nil {
            Button {
                showEditSheet = true
            } label: {
                Label("Edit Connections", systemImage: "pencil")
            }
            .sheet(isPresented: $showEditSheet) {
                EditConnectionsSheet(
                    profile: profile,
                    userNameResolver: userNameResolver,
                    allCards: allCards,
                    onSave: { reports, peers, manager in
                        onUpdateConnections?(reports, peers, manager)
                        showEditSheet = false
                    },
                    onCancel: { showEditSheet = false }
                )
            }
        }
    }

    // MARK: - Helpers

    private func colorForRole(_ role: ConnectionRole) -> Color {
        switch role {
        case .manager: return .blue
        case .orgPeer: return .orange
        case .report: return .green
        case .discoveredPeer: return .cyan
        case .iDependOn: return .purple
        case .dependsOnMe: return .pink
        case .weakSignal: return .gray
        }
    }
}

// MARK: - Supporting Types

enum ConnectionRole {
    case manager, orgPeer, report, discoveredPeer, iDependOn, dependsOnMe, weakSignal

    var label: String {
        switch self {
        case .manager: return "Manager"
        case .orgPeer: return "Peer"
        case .report: return "Report"
        case .discoveredPeer: return "Discovered Peer"
        case .iDependOn: return "I depend on"
        case .dependsOnMe: return "Depends on me"
        case .weakSignal: return "Weak signal"
        }
    }
}

struct GraphNode {
    let userID: String
    let role: ConnectionRole
    let interaction: UserInteraction?
    let isOrgConnection: Bool
}

// MARK: - Edit Connections Sheet

struct EditConnectionsSheet: View {
    let profile: UserProfile?
    let userNameResolver: (String) -> String
    let allCards: [PeopleCard]
    let onSave: ([String], [String], String) -> Void
    let onCancel: () -> Void

    @State private var selectedReports: Set<String> = []
    @State private var selectedPeers: Set<String> = []
    @State private var selectedManager: String = ""
    @State private var searchText = ""

    var body: some View {
        VStack(spacing: 16) {
            Text("Edit Connections")
                .font(.headline)
                .padding(.top, 12)

            HStack {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.secondary)
                TextField("Search people...", text: $searchText)
                    .textFieldStyle(.plain)
            }
            .padding(8)
            .background(Color.secondary.opacity(0.1), in: RoundedRectangle(cornerRadius: 8))
            .padding(.horizontal)

            List {
                Section("Manager") {
                    ForEach(usersForManager, id: \.userID) { a in
                        HStack {
                            Text("@\(userNameResolver(a.userID))")
                            Spacer()
                            if selectedManager == a.userID {
                                Image(systemName: "checkmark")
                                    .foregroundStyle(.blue)
                            }
                        }
                        .contentShape(Rectangle())
                        .onTapGesture {
                            selectedManager = selectedManager == a.userID ? "" : a.userID
                        }
                    }
                }

                Section("Key Peers") {
                    ForEach(usersForPeers, id: \.userID) { a in
                        HStack {
                            Text("@\(userNameResolver(a.userID))")
                            Spacer()
                            if selectedPeers.contains(a.userID) {
                                Image(systemName: "checkmark")
                                    .foregroundStyle(.orange)
                            }
                        }
                        .contentShape(Rectangle())
                        .onTapGesture {
                            if selectedPeers.contains(a.userID) {
                                selectedPeers.remove(a.userID)
                            } else {
                                selectedPeers.insert(a.userID)
                            }
                        }
                    }
                }

                Section("My Reports") {
                    ForEach(usersForReports, id: \.userID) { a in
                        HStack {
                            Text("@\(userNameResolver(a.userID))")
                            Spacer()
                            if selectedReports.contains(a.userID) {
                                Image(systemName: "checkmark")
                                    .foregroundStyle(.green)
                            }
                        }
                        .contentShape(Rectangle())
                        .onTapGesture {
                            if selectedReports.contains(a.userID) {
                                selectedReports.remove(a.userID)
                            } else {
                                selectedReports.insert(a.userID)
                            }
                        }
                    }
                }
            }

            HStack {
                Button("Cancel", action: onCancel)
                    .keyboardShortcut(.cancelAction)
                Spacer()
                Button("Save") {
                    onSave(
                        Array(selectedReports),
                        Array(selectedPeers),
                        selectedManager
                    )
                }
                .keyboardShortcut(.defaultAction)
            }
            .padding()
        }
        .frame(width: 400, height: 500)
        .onAppear {
            selectedReports = Set(profile?.decodedReports ?? [])
            selectedPeers = Set(profile?.decodedPeers ?? [])
            selectedManager = profile?.manager ?? ""
        }
    }

    private var availableUsers: [PeopleCard] {
        let filtered = allCards
        if searchText.isEmpty { return filtered }
        let q = searchText.lowercased()
        return filtered.filter {
            userNameResolver($0.userID).lowercased().contains(q)
        }
    }

    private var usersForManager: [PeopleCard] {
        let excluded = selectedPeers.union(selectedReports)
        return availableUsers.filter { !excluded.contains($0.userID) }
    }

    private var usersForPeers: [PeopleCard] {
        var excluded = selectedReports
        if !selectedManager.isEmpty { excluded.insert(selectedManager) }
        return availableUsers.filter { !excluded.contains($0.userID) }
    }

    private var usersForReports: [PeopleCard] {
        var excluded = selectedPeers
        if !selectedManager.isEmpty { excluded.insert(selectedManager) }
        return availableUsers.filter { !excluded.contains($0.userID) }
    }
}
