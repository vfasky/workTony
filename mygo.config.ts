// The app's configuration, which `go tool mygo` evaluates with Bun or
// Node.js 22.6+ for each command. Icons come from resources/icon.png and,
// for `mygo dev`, resources/icon-dev.png (go run ./scripts/icons makes
// them).
//
// Identifiers are reverse DNS names of yizhiai.com. `mygo dev` builds
// "WorkTony Dev" with the identifier com.yizhiai.worktony.dev,
// apart from the installed app; its menu bar names it "WorkTony Debug", as
// its windows do.
export default ({ command }: { command: string }) => ({
  name: "WorkTony",
  identifier: "com.yizhiai.worktony",
  version: "0.1.0",
  out: "build",
  // The catalogs of the app's messages: English, the source, and
  // Simplified Chinese. `go tool mygo generate` writes locales/catalog_gen.go.
  i18n: { dir: "locales", source: "en" },
  ...(command === "dev" && {
    macos: {
      infoPlist: {
        CFBundleName: "WorkTony Debug",
        CFBundleDisplayName: "WorkTony Debug",
      },
    },
  }),
});
