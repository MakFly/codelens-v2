class Codelens < Formula
  desc "Agentic memory & semantic search MCP server for Claude Code"
  homepage "https://github.com/MakFly/codelens-v2"
  version "0.2.3"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/MakFly/codelens-v2/releases/download/v0.2.3/codelens_0.2.3_darwin_arm64.tar.gz"
      sha256 "b0a938634de60e0c5e1d879cb5f4d2a60d70b835f205d912bfebceabe59ee9a4"
    else
      url "https://github.com/MakFly/codelens-v2/releases/download/v0.2.3/codelens_0.2.3_darwin_amd64.tar.gz"
      sha256 "0ad37a6ed5cfada1d449fee2ab0dcc067c14211a73f6d191bf31db4a7d9e5bfa"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/MakFly/codelens-v2/releases/download/v0.2.3/codelens_0.2.3_linux_arm64.tar.gz"
      sha256 "939f848e3615921ff6ac7bab7bd28d37ccd1ee8049e38ce1f38841e4b0a7e6ba"
    else
      url "https://github.com/MakFly/codelens-v2/releases/download/v0.2.3/codelens_0.2.3_linux_amd64.tar.gz"
      sha256 "4987f14b58478af1334fcc76a7899b3cfd4e8f63dce31ef60e21f8f69a41cd5d"
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
