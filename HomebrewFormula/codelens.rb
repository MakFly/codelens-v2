class Codelens < Formula
  desc "Agentic memory & semantic search MCP server for Claude Code"
  homepage "https://github.com/MakFly/codelens-v2"
  version "0.2.1"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/MakFly/codelens-v2/releases/download/v0.2.1/codelens_0.2.1_darwin_arm64.tar.gz"
      sha256 "REPLACE_WITH_ARM64_SHA256"
    else
      url "https://github.com/MakFly/codelens-v2/releases/download/v0.2.1/codelens_0.2.1_darwin_amd64.tar.gz"
      sha256 "REPLACE_WITH_AMD64_SHA256"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/MakFly/codelens-v2/releases/download/v0.2.1/codelens_0.2.1_linux_arm64.tar.gz"
      sha256 "REPLACE_WITH_ARM64_SHA256"
    else
      url "https://github.com/MakFly/codelens-v2/releases/download/v0.2.1/codelens_0.2.1_linux_amd64.tar.gz"
      sha256 "REPLACE_WITH_AMD64_SHA256"
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
