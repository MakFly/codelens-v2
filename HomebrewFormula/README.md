# Homebrew Formula Process

## Publishing a New Release

### 1. Build & Tag

```bash
# Update version in main.go if needed
git tag v0.x.x
git push origin v0.x.x
```

Wait for GitHub Actions to complete (~1 min).

### 2. Get SHA256 Checksums

```bash
# Download all archives
gh release download v0.x.x

# Calculate checksums
shasum -a codelens_0.x.x_darwin_amd64.tar.gz
shasum -a codelens_0.x.x_darwin_arm64.tar.gz
shasum -a codelens_0.x.x_linux_amd64.tar.gz
shasum -a codelens_0.x.x_linux_arm64.tar.gz
```

### 3. Update Formula

Edit `codelens.rb`:
- Update `version` to match tag
- Update URLs to new version
- Update SHA256 hashes

### 4. Commit & Test Locally

```bash
git add HomebrewFormula/codelens.rb
git commit -m "chore: update Homebrew formula to v0.x.x"
git push

# Test locally
brew install --debug ./HomebrewFormula/codelens.rb
```

### 5. Publish to Homebrew Tap

```bash
# Create PR to tap
gh repo fork MakFly/homebrew-codelens --clone=false 2>/dev/null || true
# Or manually submit PR to https://github.com/MakFly/homebrew-codelens
```

## Creating Homebrew Tap (First Time)

```bash
# Create new repo on GitHub: homebrew-codelens
git init homebrew-codelens
cd homebrew-codelens
git commit --allow-empty -m "Initial commit"
gh repo create homebrew-codelens --public --source=. --push

# Add formula
cp ../codelens-v2/HomebrewFormula/codelens.rb ./
git add codelens.rb
git commit -m "Add codelens formula"
git push
```

Users can then install with:
```bash
brew tap MakFly/codelens
brew install codelens
```

## Notes

- Homebrew requires HTTPS URLs
- SHA256 must be exactly 64 hex characters
- ARM64 = Apple Silicon (M1/M2/M3), AMD64 = Intel
