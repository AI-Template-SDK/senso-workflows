# Setting Up Go Development Environment for Private GitHub Repositories

This guide will help you configure your Go development environment to work with private GitHub repositories, specifically for the `senso-workflows` project which depends on the private `github.com/AI-Template-SDK/senso-api` repository.

## Prerequisites

1. **Go 1.24 or higher** - This project requires Go 1.24+
2. **Git** installed on your system
3. **SSH key** configured for your GitHub account ([GitHub SSH setup guide](https://docs.github.com/en/authentication/connecting-to-github-with-ssh))

## Configuration Steps

### 1. Configure Go to recognize private repositories

Tell Go to treat AI-Template-SDK repositories as private:

```bash
go env -w GOPRIVATE=github.com/AI-Template-SDK/*
```

This prevents Go from trying to fetch these modules from the public proxy.

### 2. Configure Git to use SSH for GitHub

Set up Git to automatically use SSH instead of HTTPS for GitHub repositories:

```bash
git config --global url."git@github.com:".insteadOf "https://github.com/"
```

This ensures that Go can authenticate when fetching private dependencies.

### 3. Verify your SSH key is working

Test your GitHub SSH connection:

```bash
ssh -T git@github.com
```

You should see a message like:
```
Hi username! You've successfully authenticated, but GitHub does not provide shell access.
```

### 4. Install dependencies

Once configured, you can install the project dependencies:

```bash
go mod download
# or
go mod tidy
```

## Troubleshooting

### Error: "terminal prompts disabled"

If you see an error like:
```
fatal: could not read Username for 'https://github.com': terminal prompts disabled
```

This means Git is trying to use HTTPS instead of SSH. Make sure you've completed step 2 above.

### Error: "Permission denied (publickey)"

If you get SSH authentication errors:
1. Check that your SSH key is added to your GitHub account
2. Ensure your SSH agent is running: `eval "$(ssh-agent -s)"`
3. Add your key to the agent: `ssh-add ~/.ssh/id_rsa` (or your key path)

### Module verification errors

If you see errors about module verification:
```
verifying module: ... 404 Not Found
```

This is expected for private repositories. The GOPRIVATE setting (step 1) should prevent these verification attempts.

### Alternative: Using Personal Access Tokens

If you prefer to use HTTPS with a Personal Access Token instead of SSH:

1. Create a GitHub Personal Access Token with `repo` scope
2. Configure Git to use the token:
   ```bash
   git config --global url."https://${GITHUB_TOKEN}:x-oauth-basic@github.com/".insteadOf "https://github.com/"
   ```
   Replace `${GITHUB_TOKEN}` with your actual token.

## Project-Specific Dependencies

This project uses the following private repositories:
- `github.com/AI-Template-SDK/senso-api` - Core API models and interfaces

## Verification

To verify your setup is working correctly:

1. Clone this repository:
   ```bash
   git clone git@github.com:AI-Template-SDK/senso-workflows.git
   cd senso-workflows
   ```

2. Download dependencies:
   ```bash
   go mod download
   ```

3. Build the project:
   ```bash
   go build -v .
   ```

If all commands complete without errors, your environment is properly configured!

## Additional Resources

- [Go Modules with Private Repos](https://go.dev/doc/faq#git_https)
- [GitHub SSH Documentation](https://docs.github.com/en/authentication/connecting-to-github-with-ssh)
- [Go Module Authentication](https://go.dev/ref/mod#private-modules)

## Need Help?

If you encounter issues not covered in this guide, please:
1. Check that you have access to the private repositories on GitHub
2. Ensure your GitHub account has the necessary permissions
3. Contact the team lead or repository administrator 