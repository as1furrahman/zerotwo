package shell

import (
	"strings"
	"testing"
)

func TestTokenizingClassifier(t *testing.T) {
	classifier := NewTokenizingClassifier()

	tests := []struct {
		name     string
		cmd      string
		expected Severity
	}{
		// Read-only commands
		{"simple ls", "ls -la", SeverityReadOnly},
		{"ls with path", "ls /var/log", SeverityReadOnly},
		{"cat file", "cat /etc/os-release", SeverityReadOnly},
		{"piped read-only", "cat /etc/os-release | grep VERSION | wc -l", SeverityReadOnly},
		{"chained read-only", "uname -a && uptime && free -h", SeverityReadOnly},
		{"df and du", "df -h ; du -sh /tmp", SeverityReadOnly},
		{"ps and grep", "ps aux | grep zerotwo", SeverityReadOnly},
		{"safe find", "find /tmp -name '*.log' -type f", SeverityReadOnly},
		{"quoted separator false positive", "ls \"a;b\"", SeverityReadOnly},
		{"quoted pipe false positive", "ls 'a|b'", SeverityReadOnly},
		{"safe top batch", "top -b -n 1", SeverityReadOnly},
		{"safe date", "date +%Y-%m-%d", SeverityReadOnly},
		{"safe ip show", "ip addr show eth0", SeverityReadOnly},
		{"safe dmesg", "dmesg -T", SeverityReadOnly},
		{"wrapper normalized", "command ls -l", SeverityReadOnly},
		{"busybox ls", "busybox ls -l", SeverityReadOnly},
		{"project config json", "cat ./config.json", SeverityReadOnly},

		// Generic (safe mutations or non-allowlisted / sensitive secrets)
		{"bare env", "env", SeverityGeneric},
		{"bare printenv", "printenv", SeverityGeneric},
		{"cat ssh key", "cat ~/.ssh/id_rsa", SeverityGeneric},
		{"cat ssh ed25519", "cat ~/.ssh/id_ed25519", SeverityGeneric},
		{"cat config.json", "cat ~/.config/zerotwo/config.json", SeverityGeneric},
		{"cat etc shadow", "cat /etc/shadow", SeverityGeneric},
		{"cat env file", "cat .env", SeverityGeneric},
		{"head pem file", "head cert.pem", SeverityGeneric},
		{"tail key file", "tail private.key", SeverityGeneric},
		{"cat proc self environ", "cat /proc/self/environ", SeverityGeneric},
		{"cat proc ppid environ", "cat /proc/$PPID/environ", SeverityGeneric},
		{"cat proc pid environ", "cat /proc/1234/environ", SeverityGeneric},
		{"cat proc mem", "cat /proc/self/mem", SeverityGeneric},
		{"cat proc cmdline", "cat /proc/self/cmdline", SeverityGeneric},
		{"cat glob ssh", "cat ~/.ss?/id_*", SeverityGeneric},
		{"cat root ssh", "cat ~root/.ssh/id_rsa", SeverityGeneric},
		{"mkdir", "mkdir -p /tmp/testdir", SeverityGeneric},
		{"touch", "touch /tmp/testfile", SeverityGeneric},
		{"git commit", "git commit -m 'test'", SeverityGeneric},
		{"sudo read-only elevated", "sudo ls -la", SeverityGeneric},
		{"env with command", "env ls -l", SeverityGeneric},
		{"ping with count non-read-only", "ping -c 3 1.1.1.1", SeverityGeneric},
		{"ping without count", "ping 1.1.1.1", SeverityGeneric},
		{"nslookup", "nslookup example.com", SeverityGeneric},
		{"dig", "dig example.com", SeverityGeneric},
		{"telnet", "telnet 192.168.1.1 80", SeverityGeneric},
		{"socat", "socat - TCP4:example.com:80", SeverityGeneric},
		{"openssl s_client", "openssl s_client -connect example.com:443", SeverityGeneric},
		{"busybox wget", "busybox wget http://example.com/payload", SeverityGeneric},
		{"apt install", "apt install curl", SeverityGeneric},
		{"apk add", "apk add curl", SeverityGeneric},
		{"dnf install", "dnf install git", SeverityGeneric},
		{"pacman -S", "pacman -S wget", SeverityGeneric},
		{"top interactive", "top", SeverityGeneric},

		// Destructive commands & evasions
		{"rm -rf", "rm -rf /tmp/junk", SeverityDestructive},
		{"rm with flag combo", "rm -fr /tmp/junk", SeverityDestructive},
		{"rm recursive long flag", "rm --recursive -f /tmp/junk", SeverityDestructive},
		{"rm quote evasion double", "\"rm\" -rf /tmp/junk", SeverityDestructive},
		{"rm quote evasion single", "'rm' -rf /tmp/junk", SeverityDestructive},
		{"rm backslash evasion", "\\rm -rf /tmp/junk", SeverityDestructive},
		{"path rm", "/bin/rm -rf /tmp/junk", SeverityDestructive},
		{"env prefix rm", "FOO=1 rm -rf /tmp/junk", SeverityDestructive},
		{"sudo destructive", "sudo rm -rf /", SeverityDestructive},
		{"dd write", "dd if=/dev/zero of=/dev/sda", SeverityDestructive},
		{"mkfs", "mkfs.ext4 /dev/sdb1", SeverityDestructive},
		{"fdisk", "fdisk /dev/sda", SeverityDestructive},
		{"chmod recursive", "chmod -R 777 /", SeverityDestructive},
		{"chown recursive", "chown -R root /home", SeverityDestructive},
		{"shutdown", "shutdown -h now", SeverityDestructive},
		{"reboot", "reboot", SeverityDestructive},
		{"init 0", "init 0", SeverityDestructive},
		{"subshell $(...)", "echo $(rm -rf /)", SeverityDestructive},
		{"backticks", "ls `rm -rf /`", SeverityDestructive},
		{"here-doc", "cat <<EOF\nrm -rf /\nEOF", SeverityDestructive},
		{"process substitution", "diff <(ls) <(ls -a)", SeverityDestructive},
		{"write redirect >", "echo hello > /tmp/out.txt", SeverityDestructive},
		{"append redirect >>", "ls >> /tmp/out.txt", SeverityDestructive},
		{"redirect to dev", "echo 1 > /dev/sda", SeverityDestructive},
		{"chained destructive", "ls -la && rm -rf /", SeverityDestructive},
		{"pipeline into destructive", "echo hi | dd of=/dev/sda", SeverityDestructive},
		{"find with -delete", "find / -name '*.tmp' -delete", SeverityDestructive},
		{"find with -exec", "find / -name '*.tmp' -exec rm {} \\;", SeverityDestructive},
		{"date setting clock", "date -s '2026-01-01'", SeverityGeneric},
		{"dmesg clearing", "dmesg -c", SeverityGeneric},
		{"sh -c wrapper", "sh -c 'echo hi'", SeverityDestructive},
		{"bash -c wrapper", "bash -c 'whoami'", SeverityDestructive},
		{"eval", "eval ls", SeverityDestructive},
		{"xargs", "ls | xargs rm", SeverityDestructive},
		{"find with -fprint", "find . -fprint x", SeverityGeneric},
		{"find with -fprintf", "find . -fprintf x '%p\n'", SeverityGeneric},
		{"command with rm", "command rm x", SeverityDestructive},
		{"command with rm -rf", "command rm -rf x", SeverityDestructive},
		{"nohup rm x", "nohup rm x", SeverityDestructive},
		{"nohup rm -rf x", "nohup rm -rf x", SeverityDestructive},
		{"nohup ls creates nohup.out", "nohup ls", SeverityGeneric},
		{"timeout 5 rm x", "timeout 5 rm x", SeverityDestructive},
		{"timeout 5 rm -rf x", "timeout 5 rm -rf x", SeverityDestructive},
		{"nice rm x", "nice rm x", SeverityDestructive},
		{"nice -n 10 rm -rf x", "nice -n 10 rm -rf x", SeverityDestructive},
		{"time rm x", "time rm x", SeverityDestructive},
		{"time rm -rf x", "time rm -rf x", SeverityDestructive},
		{"stdbuf -o0 rm x", "stdbuf -o0 rm x", SeverityDestructive},
		{"stdbuf -o0 rm -rf x", "stdbuf -o0 rm -rf x", SeverityDestructive},
		{"env rm x", "env rm x", SeverityDestructive},
		{"env rm -rf x", "env rm -rf x", SeverityDestructive},
		{"unlink command", "unlink /tmp/foo", SeverityDestructive},
		{"shred command", "shred -u /tmp/foo", SeverityDestructive},
		{"truncate command", "truncate -s 0 /tmp/foo", SeverityDestructive},
		{"command ls safe", "command ls", SeverityReadOnly},
		{"nice -n 10 ls safe", "nice -n 10 ls", SeverityReadOnly},
		{"stdbuf -o0 ls safe", "stdbuf -o0 ls", SeverityReadOnly},
		{"timeout 5 ls safe", "timeout 5 ls", SeverityReadOnly},

		// Reviewer test matrix: Overwrite, execute, kill, reset, write, delete
		{"sed in-place -i", "sed -i 's/a/b/' file.txt", SeverityDestructive},
		{"sed in-place backup", "sed -i.bak 's/a/b/' file.txt", SeverityDestructive},
		{"git reset --hard", "git reset --hard HEAD~1", SeverityDestructive},
		{"git clean -fd", "git clean -fd", SeverityDestructive},
		{"crontab -r remove", "crontab -r", SeverityDestructive},
		{"rsync --delete", "rsync -av --delete /src/ /dst/", SeverityDestructive},
		{"python3 -c code", "python3 -c \"import os; os.remove('x')\"", SeverityDestructive},
		{"perl -e code", "perl -e 'unlink \"x\"'", SeverityDestructive},
		{"awk system call", "awk 'BEGIN{system(\"ls\")}'", SeverityDestructive},
		{"kill -9", "kill -9 1234", SeverityDestructive},
		{"pkill process", "pkill -f evil", SeverityDestructive},
		{"killall proc", "killall nginx", SeverityDestructive},
		{"mv file", "mv old.txt new.txt", SeverityGeneric},
		{"cp file", "cp src.txt dst.txt", SeverityGeneric},
		{"ln -sf symlink", "ln -sf target link", SeverityGeneric},
		{"curl -o output", "curl -o out.bin https://example.com/file", SeverityGeneric},
		{"tar -x extract", "tar -xzf archive.tar.gz", SeverityGeneric},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sev, reason := classifier.Classify(tc.cmd)
			if sev != tc.expected {
				t.Errorf("command %q: expected severity %v, got %v (reason: %s)", tc.cmd, tc.expected, sev, reason)
			}
		})
	}
}

func FuzzTokenizingClassifier(f *testing.F) {
	seeds := []string{
		"ls -la",
		"rm -rf /",
		"cat /etc/passwd | grep root",
		"echo \"hello; rm -rf /\"",
		"sudo rm -rf /",
		"time nice -n 10 timeout 5s rm -rf /tmp/foo",
		"$(whoami)",
		"`id`",
		"find / -name '*.tmp' -delete",
		"sed -i 's/a/b/' file.txt",
		"python3 -c 'import os'",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	classifier := NewTokenizingClassifier()
	f.Fuzz(func(t *testing.T, cmd string) {
		// Must never panic on arbitrary inputs
		sev, _ := classifier.Classify(cmd)
		if sev < SeverityReadOnly || sev > SeverityDestructive {
			t.Errorf("invalid severity returned: %v", sev)
		}

		// Security Invariant Property:
		// Commands with write redirections (> outside quotes), command substitutions ($(...), `...`),
		// process substitution (<(...), >(...)), or unterminated quotes must NEVER be classified as SeverityReadOnly.
		if hasWriteRedirection(cmd) || strings.Contains(cmd, "$(") || strings.Contains(cmd, "`") || strings.Contains(cmd, "<(") || strings.Contains(cmd, ">(") || hasUnbalancedQuotes(cmd) {
			if sev == SeverityReadOnly {
				t.Errorf("CRITICAL SECURITY INVARIANT VIOLATION: command %q contains redirection or substitution but classified as SeverityReadOnly", cmd)
			}
		}
	})
}
