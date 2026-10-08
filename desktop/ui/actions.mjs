// Modal dialogs make the footer inert. Both visible STOP controls must reach
// the same emergency operation without confirming Quit or Update first.
export function bindEmergencyControls(buttons, dialog, stop) {
  for (const button of buttons) button.addEventListener("click", () => {
    runEmergency(() => { if (dialog.open) dialog.close(); }, stop);
  });
}

// Shared by the React footer and shadcn Dialog: cancelling a modal cannot turn
// an emergency into its pending Quit/Update confirmation.
export function runEmergency(closeDialog, stop) {
  closeDialog();
  return stop();
}
