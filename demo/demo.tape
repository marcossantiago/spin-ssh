# Run demo/record.sh, not this file directly: it sets up the made-up sites first.
Output demo/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 620
Set Padding 16
Set Theme "Catppuccin Mocha"
Set TypingSpeed 90ms

Hide
Type "source demo/.work/env.sh && clear"
Enter
Show

Type "spin-ssh"
Sleep 400ms
Enter
Sleep 1.8s

Down
Sleep 500ms
Down
Sleep 700ms

Type "stag blog"
Sleep 1.5s
Enter
Sleep 1.5s

Type "ls"
Sleep 300ms
Enter
Sleep 1.5s
Type "exit"
Enter
Sleep 800ms
