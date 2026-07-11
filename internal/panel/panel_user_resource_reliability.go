package panel

// panelUserResourceReliabilityJS coordinates user mutations with the two
// independently loaded resource tables. Updating or deleting a user revokes
// that user's sessions on the server, so a GET started before the mutation must
// not repaint either table after the mutation completes.
func panelUserResourceReliabilityJS() string {
	return `
    function invalidateUserResourceLoads() {
      usersLoadGeneration += 1;
      sessionsLoadGeneration += 1;
    }

    const baseSetUserMutationControlsDisabledForResources = setUserMutationControlsDisabled;
    setUserMutationControlsDisabled = function(disabled) {
      baseSetUserMutationControlsDisabledForResources(disabled);
      const loadSessionsButton = document.getElementById('btn-load-sessions');
      if (loadSessionsButton) loadSessionsButton.disabled = Boolean(disabled) || isViewerRole();
    };

    const baseWithUserMutationForResources = withUserMutation;
    withUserMutation = async function(action) {
      if (userMutationInFlight) return null;
      invalidateUserResourceLoads();
      return baseWithUserMutationForResources(action);
    };
`
}
