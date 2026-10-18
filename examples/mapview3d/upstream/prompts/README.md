# Next performance round: project prompts

Give each owning project its corresponding prompt:

- [nt3d](nt3d.md): highest-priority sampling hot path, then color conversion.
- [ntgpuimage](ntgpuimage.md): specialized mipmap construction.
- [ntcharts3d](ntcharts3d.md): presentation measurement and safe depth-scratch reuse.

The first two tasks can proceed independently if mip output semantics stay
unchanged. ntcharts3d owns coordinating renderer-lifetime changes with nt3d;
its initial measurement work can proceed immediately. No separate ntpicture
implementation task is justified yet: the presentation measurements should
establish whether one is needed.

Each prompt asks agents to preserve sibling working trees, distinguish tested
facts from proposed gains, and provide consumer measurements before release.
