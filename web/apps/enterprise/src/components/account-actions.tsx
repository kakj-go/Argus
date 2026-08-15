import { AppearanceControls } from "@argus/ui";
import { UserMenu } from "./user-menu";

/** Enterprise portal account controls shared by chat and administration. */
export function AccountActions() {
  return (
    <div className="argus-account-actions">
      <AppearanceControls />
      <UserMenu />
    </div>
  );
}
