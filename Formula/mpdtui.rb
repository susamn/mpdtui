# Reference copy -- the live formula Homebrew actually installs from
# lives in the susamn/homebrew-mpdtui tap (github.com/susamn/homebrew-mpdtui).
# Keep the two in sync by hand: bump `url`/`sha256` here on every
# release, then copy this file to that repo's Formula/mpdtui.rb. Also
# bump internal/version/VERSION to match (shown in the app's own Stats
# box border) -- unrelated to Homebrew, but the same by-hand release
# step is a natural place not to forget it.
class Mpdtui < Formula
  desc "Lazygit-style terminal UI for MPD (Music Player Daemon)"
  homepage "https://github.com/susamn/mpdtui"
  url "https://github.com/susamn/mpdtui/archive/refs/tags/v1.9.1.tar.gz"
  sha256 "bfc4e281d422b4a27bdf751e574cbe8672998f3c3db025b00b4e1943edcfea7e"
  license "MIT"
  head "https://github.com/susamn/mpdtui.git", branch: "main"

  depends_on "go" => :build

  def install
    system "go", "build", "-o", bin/"mpdtui", "-ldflags", "-s -w", "./cmd/mpdtui"
  end

  test do
    ENV["MPD_HOST"] = "127.0.0.1"
    ENV["MPD_PORT"] = "1"
    output = shell_output("#{bin}/mpdtui 2>&1", 1)
    assert_match "connect to MPD", output
  end
end
