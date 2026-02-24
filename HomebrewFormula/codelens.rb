class Codelens < Formula
  desc "Agentic memory & semantic search MCP server for Claude Code"
  homepage "https://github.com/MakFly/codelens-v2"
  version "0.2.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/MakFly/codelens-v2/releases/download/v0.2.0/codelens_0.2.0_darwin_arm64.tar.gz"
      sha256 "4d86361ffea60ba11561d0e8c1f07aa2f747c7817ad8d670fa43f8d6e6072015"
    else
      url "https://github.com/MakFly/codelens-v2/releases/download/v0.2.0/codelens_0.2.0_darwin_amd64.tar.gz"
      sha256 "0a04b2a771fa13d09df49270c86fd967ea75dfe80a3833386e6ac3dce5f5e380"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/MakFly/codelens-v2/releases/download/v0.2.0/codelens_0.2.0_linux_arm64.tar.gz"
      sha256 "abcdaf7f8463c5fc95ec852a5aa5297b5e86b53b2ed954b7ef22ae4ffe1657f6"
    else
      url "https://github.com/MakFly/codelens-v2/releases/download/v0.2.0/codelens_0.2.0_linux_amd64.tar.gz"
      sha256 "35e8ddbc465cd209a1d6133e5bc76f1c7c9af887e25c64773c15742fc34e8cef"
    end
  end

  def install
    bin.install "codelens"
    bin.install "codelens-hook" if OS.mac?
  end

  test do
    assert_match "CodeLens", shell_output("#{bin}/codelens version")
  end
end
