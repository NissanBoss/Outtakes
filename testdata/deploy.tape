# The deploy demo for the README.
Output deploy.gif

Require git

Set Shell "bash"
Set FontSize 22
Set Width 1200
Set Height 600
Set TypingSpeed 60ms

Env AWS_PROFILE "acme-prod"

Hide
Type "export DEPLOY_TOKEN=correct-horse-battery-staple && cd ~/src/billing"
Enter
Type "clear"
Enter
Show

Type "make deploy ENV=staging"
Sleep 500ms
Enter
Sleep 3s

Type "git push origin main --force"
Sleep 300ms
Backspace 8
Enter
Sleep 2s
