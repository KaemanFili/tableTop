# Go architecture diagrams

These diagrams describe the current package imports, main structs/interfaces,
and ownership of the Go files. Go uses structs and interfaces; the class diagram
shows their relationships. Arrows in the package diagram mean “imports.”

## Package dependencies

This graph includes every application package and its direct imports of other
application packages. Standard-library dependencies, third-party dependencies,
and test-only imports are omitted. Root `plugins` holds embedded content;
`internal/plugins` loads and validates content.

```mermaid
flowchart TD
    Main["main.go"]
    Tables["internal/tables"]
    Handlers["internal/handlers"]
    Auth["internal/auth"]
    Plugins["internal/plugins"]
    Persistence["internal/app/persistence"]
    Model["internal/app/model"]
    Artwork["internal/artwork"]
    Bundled["plugins — embedded content"]

    Main --> Tables
    Main --> Handlers
    Main --> Auth
    Main --> Plugins
    Main --> Persistence
    Main --> Model
    Tables --> Handlers
    Tables --> Auth
    Tables --> Plugins
    Tables --> Persistence
    Tables --> Model
    Handlers --> Auth
    Handlers --> Persistence
    Handlers --> Model
    Handlers --> Artwork
    Auth --> Persistence
    Plugins --> Persistence
    Plugins --> Model
    Plugins --> Artwork
    Plugins --> Bundled
    Persistence --> Model
    Model --> Bundled
```

## Main structs and interfaces

The two diagrams separate board handling from authentication and storage so the
relationships remain readable. An open diamond means a struct holds a reference
or collection. A dashed arrow to an interface means the type implements it.

```mermaid
classDiagram
    class TablesManager["tables.Manager"] {
        +RegisterRoutes()
    }
    class managedBoard {
        +ServeHTTP()
        -stop()
    }
    class EndpointHandler {
        +RegisterRoutes()
        -rollDice()
        -loadMap()
    }
    class WebSocketHandler {
        +RegisterRoutes()
        +Close()
        -boardMutex
        -positionMutex
    }
    class AppState {
        +AddUnit()
        +UpdateUnitPosition()
        +SetUnitStatus()
        +Units()
    }
    class socketWriter {
        -send()
        -run()
        -stop()
    }
    class TableLibrary
    class UnitLibrary
    class MapLibrary
    class UnitData
    class SavedBoard
    class PenStroke
    class FogMark
    class BoardRuler

    TablesManager o-- TableLibrary : membership records
    TablesManager "1" o-- "*" managedBoard : active boards
    managedBoard o-- WebSocketHandler : connections
    managedBoard ..> EndpointHandler : HTTP route dispatch
    EndpointHandler o-- AppState : shared live state
    EndpointHandler o-- WebSocketHandler : board broadcasts and snapshots
    EndpointHandler o-- UnitLibrary
    EndpointHandler o-- MapLibrary
    WebSocketHandler o-- AppState : same live state
    WebSocketHandler "1" o-- "*" socketWriter : outgoing queues
    AppState "1" o-- "*" UnitData : placed tokens
    WebSocketHandler o-- PenStroke : live drawings
    WebSocketHandler o-- FogMark : fog history
    WebSocketHandler o-- BoardRuler : active rulers
    SavedBoard o-- UnitData
    SavedBoard o-- PenStroke
    SavedBoard o-- FogMark
    SavedBoard o-- BoardRuler
```

```mermaid
classDiagram
    class AuthManager["auth.Manager"]
    class Provider {
        <<interface>>
        +User()
        +Endpoint()
        +Scopes()
    }
    class discordProvider
    class User {
        +ID
        +Name
        +AvatarURL
    }
    class Access {
        <<interface>>
        +Allowed()
        +Admit()
        +CreateInvitation()
    }
    class AccessLibrary
    class StorageLimits
    class UnitLibrary
    class MapLibrary
    class LibraryStore {
        <<interface>>
        +Maps()
        +Units()
        +DeleteScope()
    }
    class MongoStore
    class DocumentStore {
        <<interface>>
        +Load()
        +Initialize()
        +Put()
        +Delete()
        +Exists()
    }
    class mongoDocumentStore
    class MapDocumentStore {
        <<interface>>
        +List()
        +Get()
        +InitializeEach()
    }
    class mongoMapStore

    AuthManager o-- Provider : configured provider
    Provider <|.. discordProvider
    Provider ..> User : returns account profile
    AuthManager o-- Access : admission checks
    Access <|.. AccessLibrary
    AccessLibrary o-- StorageLimits
    UnitLibrary o-- StorageLimits
    MapLibrary o-- StorageLimits
    LibraryStore <|.. MongoStore
    MongoStore ..> mongoDocumentStore : unit store
    MongoStore ..> mongoMapStore : map store
    DocumentStore <|.. mongoDocumentStore
    DocumentStore <|-- MapDocumentStore
    MapDocumentStore <|.. mongoMapStore
    UnitLibrary o-- DocumentStore : optional backend
    MapLibrary o-- DocumentStore : optional backend
```

`DocumentStore` is generic in Go; its type parameter is omitted here. With no
MongoDB backend, the libraries use local JSON files. Artwork stays in local
files for both configurations. Live drawings belong to `WebSocketHandler`,
while `AppState` holds placed tokens and status definitions. `SavedBoard` is the
snapshot used to persist both kinds of content.

## Go file ownership

This graph shows where methods for the main board types live. Arrows mean
“defines the type” or “contains methods for that type,” as labeled. The grouped
file lists cover related production code rather than individual test files.

```mermaid
flowchart LR
    subgraph HTTP["internal/handlers — HTTP"]
        EndpointType["EndpointHandler"]
        EndpointFile["endpoints.go"]
        EndpointFeatures["dice.go, images.go, library.go,<br/>maps.go, copy_map.go, board.go,<br/>load_map.go, resize.go, status.go, ping.go"]
        EndpointFile -->|defines| EndpointType
        EndpointFeatures -->|methods| EndpointType
    end

    subgraph Realtime["internal/handlers — realtime"]
        SocketType["WebSocketHandler"]
        SocketFile["websocket.go"]
        Connections["socket_connection.go"]
        Positions["socket_positions.go"]
        BoardSocket["socket_board.go"]
        Tools["pen.go, fog.go, ruler.go,<br/>presence.go, board.go,<br/>load_map.go, resize.go"]
        WriterFile["socket_writer.go"]
        SocketFile -->|defines| SocketType
        Connections -->|connection methods| SocketType
        Positions -->|movement methods| SocketType
        BoardSocket -->|message methods| SocketType
        Tools -->|feature methods| SocketType
        SocketType -->|queues writes through| WriterFile
    end

    subgraph Domain["internal/app/model"]
        StateType["AppState"]
        SavedType["SavedBoard"]
        StateFile["state.go"]
        StateRules["size.go, status.go,<br/>session_status.go"]
        Snapshots["board.go, board_validation.go,<br/>limits.go"]
        Definitions["units.go, maps.go,<br/>image_fit.go, artwork.go, content.go"]
        StateFile -->|defines| StateType
        StateRules -->|methods| StateType
        Snapshots -->|define and validate| SavedType
        Definitions -->|token and artwork data| StateType
    end

    EndpointType -->|uses shared state| StateType
    SocketType -->|uses shared state| StateType
    EndpointType -->|coordinates board updates| SocketType
```

Some files contain methods on multiple types. For example, `board.go` handles
the HTTP clear request and the socket-owned state reset. Keeping these methods
in the same package currently lets them share the board lock.

## Source entry points

| Area | Start here |
| --- | --- |
| Startup and route wiring | [main.go](../main.go) |
| Tabletop manager and board lifecycle | [tables.go](../internal/tables/tables.go), [board.go](../internal/tables/board.go) |
| HTTP board handler | [endpoints.go](../internal/handlers/endpoints.go) |
| Realtime handler | [websocket.go](../internal/handlers/websocket.go), [socket_board.go](../internal/handlers/socket_board.go) |
| Live token state | [state.go](../internal/app/model/state.go) |
| Saved board data | [board.go](../internal/app/model/board.go) |
| Unit and map libraries | [library.go](../internal/app/persistence/library.go), [maps.go](../internal/app/persistence/maps.go) |
| Storage interfaces and MongoDB | [document_store.go](../internal/app/persistence/document_store.go), [mongo.go](../internal/app/persistence/mongo.go) |
| Accounts and provider interface | [auth.go](../internal/auth/auth.go), [provider.go](../internal/auth/provider.go) |
| Invitations and admission | [access.go](../internal/app/persistence/access.go) |

The package graph can be checked against `go list -json ./...`: compare each
package's `Imports` entries beginning with `tableTop/main`. Update the struct
and file diagrams when moving a type or changing which component holds its
state. See [architecture guidance](architecture.md) for responsibilities and
rules for further extraction.
