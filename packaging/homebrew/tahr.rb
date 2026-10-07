class Tahr < Formula
  desc "Modern, Blazing-Fast Terminal & Graphical IDE built in Go"
  homepage "https://github.com/baibeicha/tahr"
  version "0.2.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/baibeicha/tahr/releases/download/v#{version}/tahr-darwin-arm64.tar.gz"
    else
      url "https://github.com/baibeicha/tahr/releases/download/v#{version}/tahr-darwin-amd64.tar.gz"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/baibeicha/tahr/releases/download/v#{version}/tahr-linux-arm64.tar.gz"
    else
      url "https://github.com/baibeicha/tahr/releases/download/v#{version}/tahr-linux-amd64.tar.gz"
    end
  end

  def install
    bin.install "tahr"
  end

  test do
    assert_match "Tahr", shell_output("#{bin}/tahr --help", 0)
  end
end
