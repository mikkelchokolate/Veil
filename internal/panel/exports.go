package panel

const ApplyCardPlaceholder = panelApplyCardPlaceholder
const ApplyActionsPlaceholder = panelApplyActionsPlaceholder
const ClientLinksCardPlaceholder = panelClientLinksCardPlaceholder
const ClientProfileControlsPlaceholder = panelClientProfileControlsPlaceholder
const ClientProfileActionsPlaceholder = panelClientProfileActionsPlaceholder
const InboundFormPlaceholder = panelInboundFormPlaceholder
const IntroCardsPlaceholder = panelIntroCardsPlaceholder
const RoutingCardPlaceholder = panelRoutingCardPlaceholder
const RuntimeStatsCardsPlaceholder = panelRuntimeStatsCardsPlaceholder
const SettingsCardPlaceholder = panelSettingsCardPlaceholder
const WarpCardPlaceholder = panelWarpCardPlaceholder
const ClientLinksActionsPlaceholder = panelClientLinksActionsPlaceholder
const InboundActionsPlaceholder = panelInboundActionsPlaceholder
const DynamicFieldsPlaceholder = panelDynamicFieldsPlaceholder
const IntroActionsPlaceholder = panelIntroActionsPlaceholder
const RoutingActionsPlaceholder = panelRoutingActionsPlaceholder
const RuntimeStatsActionsPlaceholder = panelRuntimeStatsActionsPlaceholder
const SettingsActionsPlaceholder = panelSettingsActionsPlaceholder
const UtilityActionsPlaceholder = panelUtilityActionsPlaceholder
const WarpActionsPlaceholder = panelWarpActionsPlaceholder
const UsersCardPlaceholder = panelUsersCardPlaceholder
const UsersActionsPlaceholder = panelUsersActionsPlaceholder
const BackupsCardPlaceholder = panelBackupsCardPlaceholder
const BackupsActionsPlaceholder = panelBackupsActionsPlaceholder

func ApplyCardHTML() string                   { return panelApplyCardHTML() }
func ApplyActionsJS() string                  { return panelApplyActionsJS() }
func ClientLinksCardHTML() string             { return panelClientLinksCardHTML() }
func ClientLinksActionsJS() string            { return panelClientLinksActionsJS() }
func ClientProfileControlsHTML() string       { return panelClientProfileControlsHTML() }
func ClientProfileActionsJS() string          { return panelClientProfileActionsJS() }
func InboundFormHTML() string                 { return panelInboundFormHTML() }
func InboundActionsJS() string                { return panelInboundActionsJS() }
func DynamicFieldsJS() string                 { return panelDynamicFieldsJS() }
func InboundProtocolOptionsHTML() string      { return panelInboundProtocolOptionsHTML() }
func InboundTransportOptionsHTML() string     { return panelInboundTransportOptionsHTML() }
func InboundProtocolTransportRulesJS() string { return panelInboundProtocolTransportRulesJS() }
func IntroCardsHTML() string                  { return panelIntroCardsHTML() }
func IntroActionsJS() string                  { return panelIntroActionsJS() }
func RoutingCardHTML() string                 { return panelRoutingCardHTML() }
func RoutingActionsJS() string                { return panelRoutingActionsJS() }
func RuntimeStatsCardsHTML() string           { return panelRuntimeStatsCardsHTML() }
func RuntimeStatsActionsJS() string           { return panelRuntimeStatsActionsJS() }
func SettingsCardHTML() string                { return panelSettingsCardHTML() }
func SettingsActionsJS() string               { return panelSettingsActionsJS() }
func UtilityActionsJS() string                { return panelUtilityActionsJS() }
func WarpCardHTML() string                    { return panelWarpCardHTML() }
func WarpActionsJS() string                   { return panelWarpActionsJS() }
func UsersCardHTML() string                   { return panelUsersCardHTML() }
func UsersActionsJS() string                  { return panelUsersActionsJS() + panelUsersReliabilityJS() }
func BackupsCardHTML() string                 { return panelBackupsCardHTML() }
func BackupsActionsJS() string                { return panelBackupsActionsJS() + panelBackupsReliabilityJS() }
