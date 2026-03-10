-- Rename the session column from "activeOrg" to "activeOrganizationId"
-- to match what BetterAuth's organization plugin expects.
ALTER TABLE "session" RENAME COLUMN "activeOrg" TO "activeOrganizationId";
